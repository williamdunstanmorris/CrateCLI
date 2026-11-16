package digest

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/williamdunstanmorris/cratecli/internal/core"
	"github.com/williamdunstanmorris/cratecli/internal/library"
	"github.com/williamdunstanmorris/cratecli/internal/mail"
)

// Runner sends digests and keeps the run log.
type Runner struct {
	Service *core.Service
	File    *File
	Sender  mail.Sender
	// Host names this machine in the footer and the run log.
	Host string
	Now  func() time.Time
}

// NewRunner fills in Host and Now.
func NewRunner(svc *core.Service, f *File, s mail.Sender) *Runner {
	host, _ := os.Hostname()
	return &Runner{Service: svc, File: f, Sender: s, Host: host, Now: time.Now}
}

// SendOptions tweak one send.
type SendOptions struct {
	To        []string // replaces the digest's recipients (names or addresses)
	DryRun    bool     // build and render, but don't send or mark seen
	SendEmpty bool     // send even when nothing is new
}

// Result says what a send did.
type Result struct {
	Issue   *Issue
	Message mail.Message
	Run     library.DigestRun
}

// Send builds, renders and sends one digest, one email per recipient so
// addresses stay private. On success its records are marked seen and the
// run is logged.
func (r *Runner) Send(ctx context.Context, d Digest, o SendOptions) (*Result, error) {
	started := r.Now()
	loc, err := r.File.Location()
	if err != nil {
		return nil, err
	}
	run := library.DigestRun{Digest: d.Name, Date: started.In(loc).Format("2006-01-02"), StartedAt: started, Host: r.Host}
	fail := func(err error) (*Result, error) {
		run.Status, run.Message, run.FinishedAt = library.RunFailed, err.Error(), r.Now()
		r.log(ctx, run)
		return nil, err
	}

	to, err := r.File.Addresses(d, o.To)
	if err != nil {
		return fail(err)
	}
	if r.File.From == "" {
		return fail(fmt.Errorf("digests.yaml has no from: address"))
	}
	// Digests never repeat records, whatever kind of dig a section is.
	for i := range d.Sections {
		d.Sections[i].Dig.HideSeen = true
	}
	is, err := Build(ctx, r.Service, d, started.In(loc))
	if err != nil {
		return fail(err)
	}
	msg, err := Render(is, r.File.From, r.Host)
	if err != nil {
		return fail(err)
	}
	res := &Result{Issue: is, Message: msg}
	run.Recipients, run.Records = len(to), len(is.Records())

	switch {
	case o.DryRun:
		run.Status = library.RunDryRun
	case is.Empty() && !o.SendEmpty:
		run.Status, run.Message = library.RunSkipped, "nothing new"
	default:
		for _, addr := range to {
			m := msg
			m.To = []string{addr}
			if err := r.Sender.Send(ctx, m); err != nil {
				return fail(fmt.Errorf("sending to %s: %w", addr, err))
			}
		}
		run.Status = library.RunSent
		// Logged as a dig, so the email's numbers work with `cratecli save`.
		if _, err := r.Service.Record(ctx, "digest "+d.Name, is.Records(), true); err != nil {
			run.Message = "sent, but marking records seen failed: " + err.Error()
		}
	}
	run.FinishedAt = r.Now()
	res.Run = run
	if o.DryRun {
		return res, nil
	}
	r.log(ctx, run)
	return res, nil
}

func (r *Runner) log(ctx context.Context, run library.DigestRun) {
	if lib := r.Service.Library; lib != nil {
		if _, err := lib.AddRun(ctx, run); err != nil && r.Service.Log != nil {
			r.Service.Log("warn: logging digest run: %v", err)
		}
	}
}

// Due returns the digests scheduled for today whose time has passed and
// that haven't been sent today.
func (r *Runner) Due(ctx context.Context) ([]Digest, error) {
	loc, err := r.File.Location()
	if err != nil {
		return nil, err
	}
	now := r.Now().In(loc)
	var out []Digest
	for _, d := range r.File.Digests {
		at, ok := d.ScheduledOn(now, loc)
		if !ok || now.Before(at) {
			continue
		}
		if lib := r.Service.Library; lib != nil {
			sent, err := lib.SentOn(ctx, d.Name, now.Format("2006-01-02"))
			if err != nil {
				return nil, err
			}
			if sent {
				continue
			}
		}
		out = append(out, d)
	}
	return out, nil
}
