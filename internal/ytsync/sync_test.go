package ytsync

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/williamdunstanmorris/cratecli/internal/crate"
	"github.com/williamdunstanmorris/cratecli/internal/library"
	"github.com/williamdunstanmorris/cratecli/internal/youtube"
)

func TestParseTitle(t *testing.T) {
	for _, c := range []struct{ title, channel, artist, track string }{
		{"Burial - Archangel (Official Video)", "Hyperdub", "Burial", "Archangel"},
		{"Archangel", "Burial - Topic", "Burial", "Archangel"},
		{"Chez Damier – Can You Feel It (MK New York Dub) [1992]", "x", "Chez Damier", "Can You Feel It (MK New York Dub) [1992]"},
		{"Moodymann - Shades Of Jae [HD]", "x", "Moodymann", "Shades Of Jae"},
		{"Live at Sonar 2019", "Some Channel", "", "Live at Sonar 2019"},
		{`Kerrie — "Pressure Signal" (Official Audio)`, "x", "Kerrie", "Pressure Signal"},
	} {
		a, tr := ParseTitle(c.title, c.channel)
		if a != c.artist || tr != c.track {
			t.Errorf("ParseTitle(%q, %q) = %q, %q; want %q, %q", c.title, c.channel, a, tr, c.artist, c.track)
		}
	}
}

// fakeLikes serves likes in pages of 2, newest first.
type fakeLikes struct {
	videos []youtube.LikedVideo
	calls  int
}

func (f *fakeLikes) Likes(_ context.Context, token string) (youtube.LikesPage, error) {
	f.calls++
	start := 0
	fmt.Sscan(token, &start)
	end := min(start+2, len(f.videos))
	p := youtube.LikesPage{Videos: f.videos[start:end], Total: len(f.videos)}
	if end < len(f.videos) {
		p.Next = fmt.Sprint(end)
	}
	return p, nil
}

func vid(id, title string) youtube.LikedVideo {
	return youtube.LikedVideo{ID: id, Title: title, Channel: "ch"}
}

func TestSync(t *testing.T) {
	ctx := context.Background()
	lib, err := library.Open(filepath.Join(t.TempDir(), "lib.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer lib.Close()
	// One record already saved through cratecli: its like must not duplicate it.
	if _, err := lib.Save(ctx, []crate.Record{{Artist: "Burial", Title: "Archangel"}}, ""); err != nil {
		t.Fatal(err)
	}

	yt := &fakeLikes{videos: []youtube.LikedVideo{
		vid("a1", "Burial - Archangel (Official Video)"),
		vid("b2", "Moodymann - Shades Of Jae"),
		vid("c3", "Cat compilation"), // not music: kept anyway
	}}
	res, err := Sync(ctx, yt, lib, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res.New != 3 || res.NewSaved != 2 || res.Pages != 2 {
		t.Fatalf("first sync = %+v", res)
	}
	saved, _ := lib.Saved(ctx)
	if len(saved) != 3 {
		t.Fatalf("saved %d records, want 3", len(saved))
	}
	origins := map[string]string{}
	for _, r := range saved {
		origins[r.Title] = r.Origin
	}
	if origins["Archangel"] != "crate" || origins["Shades Of Jae"] != "youtube" || origins["Cat compilation"] != "youtube" {
		t.Errorf("origins = %v", origins)
	}

	// You like two more on YouTube; an incremental sync reads one page.
	yt.videos = append([]youtube.LikedVideo{vid("d4", "Kerrie - Pressure Signal"), vid("e5", "Ceri - Love Me Right")}, yt.videos...)
	yt.calls = 0
	res, err = Sync(ctx, yt, lib, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res.New != 2 {
		t.Fatalf("second sync = %+v", res)
	}
	// Page 1 had both new ones, so it reads page 2, finds nothing new and stops.
	if yt.calls != 2 {
		t.Errorf("incremental sync made %d calls, want 2", yt.calls)
	}

	// Unlike one, then a full sync notices.
	yt.videos = yt.videos[1:]
	res, err = Sync(ctx, yt, lib, Options{Full: true})
	if err != nil || res.Unliked != 1 {
		t.Fatalf("full sync = %+v %v", res, err)
	}
}
