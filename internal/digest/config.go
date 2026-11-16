// Package digest builds the scheduled email digests: which digs run on
// which day (a YAML file), running them, and rendering the email.
//
// Sending and scheduling live elsewhere: digest only turns a Digest into
// an Issue (the records) and an Issue into a mail.Message.
package digest

import (
	"fmt"
	"net/mail"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/williamdunstanmorris/cratecli/internal/config"
	"github.com/williamdunstanmorris/cratecli/internal/core"
)

// File is the digests.yaml file.
type File struct {
	// Timezone the schedule is in, e.g. Europe/Berlin. Default: local.
	Timezone string `yaml:"timezone" json:"timezone"`
	// From is the sender, e.g. "Crate <music@example.com>". It must be an
	// address your mail account is allowed to send from.
	From string `yaml:"from" json:"from"`
	// Recipients are named addresses that digests refer to by name.
	Recipients map[string]string `yaml:"recipients" json:"recipients"`
	Digests    []Digest          `yaml:"digests" json:"digests"`
	// Fallback: when this Mac only sends digests the server missed.
	Fallback Fallback `yaml:"fallback,omitempty" json:"fallback,omitempty"`
}

// Digest is one scheduled email.
type Digest struct {
	Name  string   `yaml:"name" json:"name"`   // used on the command line: monday, berlin…
	Title string   `yaml:"title" json:"title"` // in the subject: "Berlin shops"
	Days  []string `yaml:"days" json:"days"`   // mon, tue, … or daily
	At    string   `yaml:"at" json:"at"`       // 07:00
	To    []string `yaml:"to" json:"to"`       // recipient names or addresses
	// MaxPerSection caps rows per section in the email (default 40); the
	// full list is attached as CSV when anything is cut.
	MaxPerSection int       `yaml:"max_per_section,omitempty" json:"max_per_section,omitempty"`
	Sections      []Section `yaml:"sections" json:"sections"`
}

// Section is one dig inside a digest, with its own heading.
type Section struct {
	Title string          `yaml:"title" json:"title"`
	Dig   core.DigRequest `yaml:"dig" json:"dig"`
}

// Fallback configures `cratecli digest run --fallback`.
type Fallback struct {
	// Server is the cratecli API that normally sends the digests.
	Server string `yaml:"server,omitempty" json:"server,omitempty"`
	// Grace is how long after the scheduled time to wait for the server
	// before this machine sends instead (default 15m).
	Grace string `yaml:"grace,omitempty" json:"grace,omitempty"`
	// Token for the server's API (CRATECLI_API_TOKEN there). The env var
	// CRATECLI_API_TOKEN here works too.
	Token string `yaml:"token,omitempty" json:"-"`
}

var weekdays = map[string]time.Weekday{
	"sun": time.Sunday, "mon": time.Monday, "tue": time.Tuesday, "wed": time.Wednesday,
	"thu": time.Thursday, "fri": time.Friday, "sat": time.Saturday,
}

// DefaultPath is digests.yaml next to the config file.
func DefaultPath() (string, error) {
	if p := os.Getenv("CRATECLI_DIGESTS"); p != "" {
		return p, nil
	}
	dir, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "digests.yaml"), nil
}

// Load reads and checks a digests file.
func Load(path string) (*File, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no digests file at %s; create one with `cratecli digest init`", path)
		}
		return nil, err
	}
	var f File
	dec := yaml.NewDecoder(strings.NewReader(string(b)))
	dec.KnownFields(true) // typos in keys are errors, not silently ignored
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if err := f.check(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &f, nil
}

// check validates everything that doesn't need the network.
func (f *File) check() error {
	if _, err := f.Location(); err != nil {
		return err
	}
	if f.From != "" {
		if _, err := mail.ParseAddress(f.From); err != nil {
			return fmt.Errorf("from: %w", err)
		}
	}
	for name, addr := range f.Recipients {
		if _, err := mail.ParseAddress(addr); err != nil {
			return fmt.Errorf("recipient %s: %w", name, err)
		}
	}
	seen := map[string]bool{}
	for i, d := range f.Digests {
		if d.Name == "" {
			return fmt.Errorf("digest %d has no name", i+1)
		}
		if seen[d.Name] {
			return fmt.Errorf("two digests are called %q", d.Name)
		}
		seen[d.Name] = true
		if _, err := d.Weekdays(); err != nil {
			return fmt.Errorf("digest %s: %w", d.Name, err)
		}
		if _, _, err := d.Clock(); err != nil {
			return fmt.Errorf("digest %s: %w", d.Name, err)
		}
		if _, err := f.Addresses(d, nil); err != nil {
			return fmt.Errorf("digest %s: %w", d.Name, err)
		}
		if len(d.Sections) == 0 {
			return fmt.Errorf("digest %s has no sections", d.Name)
		}
	}
	if f.Fallback.Grace != "" {
		if _, err := time.ParseDuration(f.Fallback.Grace); err != nil {
			return fmt.Errorf("fallback.grace: %w", err)
		}
	}
	return nil
}

// Validate also checks every section's dig (source names, sections,
// filters) against the service.
func (f *File) Validate(svc *core.Service) error {
	for _, d := range f.Digests {
		for _, s := range d.Sections {
			if err := svc.Validate(s.Dig); err != nil {
				return fmt.Errorf("digest %s, section %q: %w", d.Name, s.Title, err)
			}
		}
	}
	return nil
}

// Location is the schedule's time zone.
func (f *File) Location() (*time.Location, error) {
	if f.Timezone == "" {
		return time.Local, nil
	}
	loc, err := time.LoadLocation(f.Timezone)
	if err != nil {
		return nil, fmt.Errorf("timezone %q: %w", f.Timezone, err)
	}
	return loc, nil
}

// Find returns the digest called name.
func (f *File) Find(name string) (Digest, error) {
	for _, d := range f.Digests {
		if strings.EqualFold(d.Name, name) {
			return d, nil
		}
	}
	names := make([]string, len(f.Digests))
	for i, d := range f.Digests {
		names[i] = d.Name
	}
	return Digest{}, fmt.Errorf("no digest %q; you have: %s", name, strings.Join(names, ", "))
}

// Addresses resolves a digest's recipients. override, when given,
// replaces the digest's own list (e.g. --to for a test send).
func (f *File) Addresses(d Digest, override []string) ([]string, error) {
	list := d.To
	if len(override) > 0 {
		list = override
	}
	if len(list) == 0 {
		return nil, fmt.Errorf("no recipients (set `to:`)")
	}
	var out []string
	for _, to := range list {
		if addr, ok := f.Recipients[to]; ok {
			out = append(out, addr)
			continue
		}
		if _, err := mail.ParseAddress(to); err != nil {
			return nil, fmt.Errorf("%q is neither a recipient name nor an email address", to)
		}
		out = append(out, to)
	}
	return out, nil
}

// Weekdays is the days the digest goes out.
func (d Digest) Weekdays() ([]time.Weekday, error) {
	if len(d.Days) == 0 {
		return nil, fmt.Errorf("no days (e.g. days: [mon])")
	}
	var out []time.Weekday
	for _, day := range d.Days {
		day = strings.ToLower(strings.TrimSpace(day))
		if day == "daily" || day == "every day" {
			return []time.Weekday{0, 1, 2, 3, 4, 5, 6}, nil
		}
		wd, ok := weekdays[day]
		if !ok && len(day) > 3 {
			// Full names: monday, tuesday…
			wd, ok = weekdays[day[:3]]
			ok = ok && strings.EqualFold(wd.String(), day)
		}
		if !ok {
			return nil, fmt.Errorf("unknown day %q (use mon, tue, … or daily)", day)
		}
		out = append(out, wd)
	}
	return out, nil
}

// Clock is the time of day it goes out (default 07:00).
func (d Digest) Clock() (hour, minute int, err error) {
	at := d.At
	if at == "" {
		at = "07:00"
	}
	t, err := time.Parse("15:04", at)
	if err != nil {
		return 0, 0, fmt.Errorf("at %q: want HH:MM", d.At)
	}
	return t.Hour(), t.Minute(), nil
}

// ScheduledOn returns when the digest is due on now's day in loc, and
// whether it runs that day at all.
func (d Digest) ScheduledOn(now time.Time, loc *time.Location) (time.Time, bool) {
	now = now.In(loc)
	days, err := d.Weekdays()
	if err != nil || !slices.Contains(days, now.Weekday()) {
		return time.Time{}, false
	}
	h, m, _ := d.Clock()
	return time.Date(now.Year(), now.Month(), now.Day(), h, m, 0, 0, loc), true
}

// DisplayTitle is the title, or the name when there's none.
func (d Digest) DisplayTitle() string {
	if d.Title != "" {
		return d.Title
	}
	return d.Name
}
