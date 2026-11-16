package digest

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/williamdunstanmorris/cratecli/internal/core"
	"github.com/williamdunstanmorris/cratecli/internal/crate"
	"github.com/williamdunstanmorris/cratecli/internal/discogs"
	"github.com/williamdunstanmorris/cratecli/internal/library"
	"github.com/williamdunstanmorris/cratecli/internal/mail"
	"github.com/williamdunstanmorris/cratecli/internal/sources"
)

// fakeShop is a source that returns fixed records, so the whole
// build → render → send → mark seen path runs without the network.
type fakeShop struct{ recs []crate.Record }

func (f fakeShop) Info() sources.Info {
	return sources.Info{Name: "fake", Kind: sources.KindShop, Sections: []sources.Section{{Name: "new"}}}
}
func (f fakeShop) Fetch(context.Context, sources.Request) ([]crate.Record, error) {
	return append([]crate.Record(nil), f.recs...), nil
}

type memSender struct{ sent []mail.Message }

func (m *memSender) Send(_ context.Context, msg mail.Message) error {
	m.sent = append(m.sent, msg)
	return nil
}

func TestRunnerSendAndDue(t *testing.T) {
	ctx := context.Background()
	lib, err := library.Open(filepath.Join(t.TempDir(), "lib.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer lib.Close()
	svc := &core.Service{
		Discogs: discogs.New("", "test"),
		Library: lib,
		Sources: map[string]sources.Source{"fake": fakeShop{recs: []crate.Record{
			{Artist: "A", Title: "One"}, {Artist: "B", Title: "Two"},
		}}},
	}
	f := &File{
		Timezone:   "UTC",
		From:       "Crate <music@example.com>",
		Recipients: map[string]string{"me": "me@example.com", "you": "you@example.com"},
		Digests: []Digest{{
			Name: "tuesday", Days: []string{"tue"}, At: "07:00", To: []string{"me", "you"},
			Sections: []Section{{Title: "Fake", Dig: core.DigRequest{From: []string{"fake"}, Fast: true}}},
		}},
	}
	sender := &memSender{}
	r := NewRunner(svc, f, sender)
	tue := time.Date(2026, 11, 17, 7, 5, 0, 0, time.UTC)
	r.Now = func() time.Time { return tue }

	// Before 07:00 nothing is due; after, tuesday is.
	r.Now = func() time.Time { return tue.Add(-10 * time.Minute) }
	if due, _ := r.Due(ctx); len(due) != 0 {
		t.Fatalf("due before 07:00: %v", due)
	}
	r.Now = func() time.Time { return tue }
	due, err := r.Due(ctx)
	if err != nil || len(due) != 1 {
		t.Fatalf("due = %v %v", due, err)
	}

	res, err := r.Send(ctx, due[0], SendOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(sender.sent) != 2 || sender.sent[0].To[0] != "me@example.com" || len(sender.sent[0].To) != 1 {
		t.Fatalf("want one email per recipient, got %+v", sender.sent)
	}
	if res.Run.Status != library.RunSent || res.Run.Records != 2 {
		t.Errorf("run = %+v", res.Run)
	}
	// Sent today: no longer due. Records are now seen, so the next send is empty and skipped.
	if due, _ := r.Due(ctx); len(due) != 0 {
		t.Errorf("still due after sending: %v", due)
	}
	res, err = r.Send(ctx, f.Digests[0], SendOptions{})
	if err != nil || res.Run.Status != library.RunSkipped || len(sender.sent) != 2 {
		t.Errorf("second send = %+v %v (sent %d)", res.Run, err, len(sender.sent))
	}
	// The email's numbers are the last dig.
	if d, recs, _ := lib.LastDig(ctx); d.Command != "digest tuesday" || len(recs) != 2 {
		t.Errorf("last dig = %+v %d", d, len(recs))
	}
}
