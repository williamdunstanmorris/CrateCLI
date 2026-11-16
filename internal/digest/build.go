package digest

import (
	"context"
	"time"

	"github.com/williamdunstanmorris/cratecli/internal/core"
	"github.com/williamdunstanmorris/cratecli/internal/crate"
)

// Issue is one edition of a digest: the records each section found.
type Issue struct {
	Digest   Digest
	Date     time.Time
	Sections []IssueSection
}

// IssueSection is one section's results. Err is set when its dig failed;
// the rest of the digest still goes out.
type IssueSection struct {
	Title   string
	Records []crate.Record
	Stats   core.Stats
	Err     error
}

// Records returns every record in the issue, in email order. Their
// position in this list is the number shown in the email.
func (is *Issue) Records() []crate.Record {
	var out []crate.Record
	for _, s := range is.Sections {
		out = append(out, s.Records...)
	}
	return out
}

// Build runs each section's dig. A record that already appeared in an
// earlier section is left out of later ones.
func Build(ctx context.Context, svc *core.Service, d Digest, now time.Time) (*Issue, error) {
	is := &Issue{Digest: d, Date: now}
	shown := map[string]bool{}
	for _, s := range d.Sections {
		sec := IssueSection{Title: s.Title}
		res, err := svc.Dig(ctx, s.Dig)
		if err != nil {
			sec.Err = err
		} else {
			sec.Stats = res.Stats
			for _, r := range res.Records {
				if k := r.Key(); k != "" && shown[k] {
					continue
				} else {
					shown[k] = true
				}
				sec.Records = append(sec.Records, r)
			}
		}
		is.Sections = append(is.Sections, sec)
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	return is, nil
}

// Empty reports whether no section found anything new.
func (is *Issue) Empty() bool { return len(is.Records()) == 0 }
