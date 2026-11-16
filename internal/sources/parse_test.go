package sources

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func TestParseRSSFixtures(t *testing.T) {
	for _, f := range []string{"clone.xml", "pitchfork_best.xml", "quietus.xml"} {
		b, err := os.ReadFile("testdata/" + f)
		if err != nil {
			t.Fatal(err)
		}
		items, err := parseRSS(b)
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		if len(items) < 10 {
			t.Fatalf("%s: only %d items", f, len(items))
		}
		if items[0].Title == "" || items[0].Link == "" {
			t.Errorf("%s: empty first item %+v", f, items[0])
		}
		t.Logf("%s: %d items, first %q cats=%v thumb=%q", f, len(items), items[0].Title, items[0].Categories, items[0].Thumbnail.URL)
	}
}

func TestPitchforkArtist(t *testing.T) {
	it := rssItem{Title: "Snakes & Ladders", Link: "https://pitchfork.com/reviews/albums/armand-hammer-snakes-and-ladders/"}
	if a := pitchforkArtist(it); a != "Armand Hammer" {
		t.Errorf("from slug: got %q", a)
	}
	it.Thumbnail.URL = "https://media.pitchfork.com/photos/x/master/pass/Armand%20Hammer%20-%20Snakes%20&%20Ladders%20album%20art.jpg"
	if a := pitchforkArtist(it); a != "Armand Hammer" {
		t.Errorf("from thumbnail: got %q", a)
	}
}

func TestSplitCloneTitle(t *testing.T) {
	a, ti, f, c := splitCloneTitle("Future Beat Alliance - Hidden Emotion  (30th Anniversary Edition) (3x12inch) - FBAHE30")
	if a != "Future Beat Alliance" || ti != "Hidden Emotion (30th Anniversary Edition)" || f != "3x12inch" || c != "FBAHE30" {
		t.Errorf("got %q %q %q %q", a, ti, f, c)
	}
	a, ti, _, c = splitCloneTitle("The Optic Crux / Random XS - Djax-Up-Beats 1990-2005: The Acid Trip EP (12inch) - DJAX-UP-555")
	if a != "The Optic Crux / Random XS" || c != "DJAX-UP-555" || ti != "Djax-Up-Beats 1990-2005: The Acid Trip EP" {
		t.Errorf("got %q %q %q", a, ti, c)
	}
}

func TestUnshout(t *testing.T) {
	if got := unshout("MAHOGANY BROWN (SMOKEY VINYL)"); got != "Mahogany Brown (Smokey Vinyl)" {
		t.Errorf("got %q", got)
	}
	if got := unshout("Moodymann"); got != "Moodymann" {
		t.Errorf("mixed case should be untouched, got %q", got)
	}
}

func TestBBC(t *testing.T) {
	b, err := os.ReadFile("testdata/bbc_children.json")
	if err != nil {
		t.Fatal(err)
	}
	now, _ := time.Parse(time.RFC3339, "2026-11-28T12:00:00Z")
	eps, err := bbcEpisodes(b, now)
	if err != nil {
		t.Fatal(err)
	}
	// Episodes scheduled after "now" (4 and 11 and 18 Dec) are skipped.
	if len(eps) == 0 || eps[0].PID != "m0034cjq" {
		t.Fatalf("first broadcast episode = %+v", eps)
	}

	b, err = os.ReadFile("testdata/bbc_segments.json")
	if err != nil {
		t.Fatal(err)
	}
	recs, err := bbcRecords(b, "Late Junction", bbcProgramme{PID: "m0033nb2", Title: "18/12/2026", Broadcast: "2026-11-13T22:15:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 4 {
		t.Fatalf("want 4 music segments (speech skipped), got %d", len(recs))
	}
	r := recs[1]
	if r.Artist != "Memotone" || r.Title != "Glimpse" || r.Label != "World of Echo" || !r.TrackOnly {
		t.Errorf("record = %+v", r)
	}
	if !strings.HasPrefix(r.Note, "from “Smallest things” · Late Junction, ") || strings.Contains(r.Note, "18/12") {
		t.Errorf("note = %q", r.Note)
	}
}

// TestInfoJSON guards `cratecli sources -f json` and GET /v1/sources:
// every source's Info must encode (a func field once broke this).
func TestInfoJSON(t *testing.T) {
	for name, src := range Registry(Env{}) {
		if _, err := json.Marshal(src.Info()); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}
