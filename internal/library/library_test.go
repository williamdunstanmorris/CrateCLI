package library

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/williamdunstanmorris/cratecli/internal/crate"
)

func open(t *testing.T) *Library {
	t.Helper()
	l, err := Open(filepath.Join(t.TempDir(), "lib.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	return l
}

func titles(recs []crate.Record) []string {
	out := make([]string, len(recs))
	for i, r := range recs {
		out[i] = r.Title
	}
	return out
}

func TestSeenAndUndo(t *testing.T) {
	ctx := context.Background()
	l := open(t)
	a := crate.Record{Artist: "Basic Channel", Title: "Q 1.1", Source: "hardwax:this-week"}
	b := crate.Record{Artist: "Rhythm & Sound", Title: "Carrie", ReleaseID: 42}
	c := crate.Record{Artist: "Moodymann", Title: "Black Mahogani"}

	if _, err := l.RecordDig(ctx, "from hardwax", []crate.Record{a, b}, true); err != nil {
		t.Fatal(err)
	}
	// Same record from another shop, spelled differently; and b found by release ID.
	a2 := crate.Record{Artist: "BASIC CHANNEL", Title: "Q 1.1 (Remastered)"}
	b2 := crate.Record{Artist: "Rhythm and Sound", Title: "Carrie (12\")", ReleaseID: 42}
	b3 := crate.Record{Artist: "R&S", Title: "Other", ReleaseID: 42}
	got, hidden, err := l.Unseen(ctx, []crate.Record{a2, c, b2, b3})
	if err != nil {
		t.Fatal(err)
	}
	if hidden != 3 || len(got) != 1 || got[0].Title != "Black Mahogani" {
		t.Fatalf("unseen = %v (hidden %d)", titles(got), hidden)
	}

	// An untracked dig doesn't mark seen but is the last dig.
	if _, err := l.RecordDig(ctx, "release 1", []crate.Record{c}, false); err != nil {
		t.Fatal(err)
	}
	if got, _, _ := l.Unseen(ctx, []crate.Record{c}); len(got) != 1 {
		t.Fatal("untracked dig marked records seen")
	}
	d, recs, err := l.LastDig(ctx)
	if err != nil || d.Command != "release 1" || len(recs) != 1 || recs[0].Title != "Black Mahogani" {
		t.Fatalf("last dig = %+v %v %v", d, titles(recs), err)
	}

	// Undo goes back to the last tracked dig.
	d, n, err := l.UndoSeen(ctx)
	if err != nil || n != 2 || d.Command != "from hardwax" {
		t.Fatalf("undo = %+v %d %v", d, n, err)
	}
	if got, _, _ := l.Unseen(ctx, []crate.Record{a, b}); len(got) != 2 {
		t.Fatal("undo didn't un-see records")
	}
	if _, _, err := l.UndoSeen(ctx); err != ErrNoDigs {
		t.Fatalf("second undo: %v", err)
	}
}

func TestRecordDigKeepsTrackOnlyAndOrder(t *testing.T) {
	ctx := context.Background()
	l := open(t)
	recs := []crate.Record{
		{Artist: "A", Title: "One", TrackOnly: true},
		{Artist: "B", Title: "Two"},
		{Artist: "A", Title: "One", TrackOnly: true}, // duplicate in one dig
	}
	if _, err := l.RecordDig(ctx, "from nts x", recs, true); err != nil {
		t.Fatal(err)
	}
	_, got, err := l.LastDig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].Title != "One" || !got[0].TrackOnly || got[1].Title != "Two" {
		t.Fatalf("got %+v", got)
	}
	s, err := l.Stats(ctx)
	if err != nil || s.Seen != 2 || s.Today != 2 || len(s.RecentDig) != 1 {
		t.Fatalf("stats %+v %v", s, err)
	}
}

func TestReopenKeepsData(t *testing.T) {
	ctx := context.Background()
	p := filepath.Join(t.TempDir(), "lib.db")
	l, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	l.RecordDig(ctx, "x", []crate.Record{{Artist: "A", Title: "B"}}, true)
	l.Close()
	l, err = Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if got, _, _ := l.Unseen(ctx, []crate.Record{{Artist: "a", Title: "b"}}); len(got) != 0 {
		t.Fatal("seen history lost on reopen")
	}
}

func TestSaveUnsave(t *testing.T) {
	ctx := context.Background()
	l := open(t)
	a := crate.Record{Artist: "Basic Channel", Title: "Q 1.1"}
	b := crate.Record{Artist: "Moodymann", Title: "Black Mahogani", TrackOnly: true}

	res, err := l.Save(ctx, []crate.Record{a, b}, "")
	if err != nil || res[0] != Added || res[1] != Added {
		t.Fatalf("save = %v %v", res, err)
	}
	// Saving again with a note updates the note; enriched data replaces old.
	a.ReleaseID = 7
	res, err = l.Save(ctx, []crate.Record{a}, "for sunday")
	if err != nil || res[0] != NoteUpdated {
		t.Fatalf("resave = %v %v", res, err)
	}
	if res, _ = l.Save(ctx, []crate.Record{b}, ""); res[0] != AlreadySaved {
		t.Fatalf("resave without note = %v", res)
	}
	got, err := l.Saved(ctx)
	if err != nil || len(got) != 2 {
		t.Fatalf("saved = %v %v", got, err)
	}
	var bc crate.Record
	for _, r := range got {
		if r.Artist == "Basic Channel" {
			bc = r
		}
	}
	if bc.MyNote != "for sunday" || bc.ReleaseID != 7 || bc.SavedAt == "" {
		t.Fatalf("saved record = %+v", bc)
	}
	// Saved counts as seen.
	if unseen, _, _ := l.Unseen(ctx, []crate.Record{a, b}); len(unseen) != 0 {
		t.Fatal("saved records not marked seen")
	}
	if n, err := l.Unsave(ctx, []crate.Record{a}); n != 1 || err != nil {
		t.Fatalf("unsave = %d %v", n, err)
	}
	if got, _ := l.Saved(ctx); len(got) != 1 || got[0].Artist != "Moodymann" || !got[0].TrackOnly {
		t.Fatalf("after unsave = %+v", got)
	}
}

func TestDigestRuns(t *testing.T) {
	ctx := context.Background()
	l := open(t)
	now := time.Now()
	for _, st := range []string{RunFailed, RunSent} {
		if _, err := l.AddRun(ctx, DigestRun{Digest: "monday", Date: "2026-11-16", StartedAt: now, FinishedAt: now, Status: st}); err != nil {
			t.Fatal(err)
		}
	}
	if ok, _ := l.SentOn(ctx, "monday", "2026-11-16"); !ok {
		t.Error("SentOn = false after a sent run")
	}
	if ok, _ := l.SentOn(ctx, "monday", "2026-11-23"); ok {
		t.Error("SentOn true for another day")
	}
	runs, err := l.Runs(ctx, "monday", "", 10)
	if err != nil || len(runs) != 2 || runs[0].Status != RunSent {
		t.Fatalf("runs = %+v %v", runs, err)
	}
	if r, ok, _ := l.LastSent(ctx, "monday"); !ok || r.Date != "2026-11-16" {
		t.Errorf("last sent = %+v %v", r, ok)
	}
}
