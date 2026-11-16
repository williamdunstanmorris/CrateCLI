package output

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"strings"
	"testing"

	"github.com/williamdunstanmorris/cratecli/internal/crate"
)

var recs = []crate.Record{{
	ReleaseID: 1, Artist: "The Persuader", Title: "Stockholm",
	Styles: []string{"Deep House"}, DiscogsURL: "https://www.discogs.com/release/1",
	Videos:           []crate.Link{{URL: "https://www.youtube.com/watch?v=a", Title: "Östermalm", Source: "discogs"}},
	YouTubeSearchURL: "https://www.youtube.com/results?search_query=x",
}}

func TestFormats(t *testing.T) {
	for _, f := range Formats {
		var b bytes.Buffer
		if err := Write(&b, f, recs); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		if b.Len() == 0 {
			t.Fatalf("%s: empty output", f)
		}
	}
	if err := Write(&bytes.Buffer{}, "nope", recs); err == nil {
		t.Fatal("expected error for unknown format")
	}
}

func TestJSONRoundTrip(t *testing.T) {
	var b bytes.Buffer
	_ = Write(&b, "json", recs)
	var got []crate.Record
	if err := json.Unmarshal(b.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got[0].Videos[0].Title != "Östermalm" {
		t.Fatalf("unexpected: %+v", got[0])
	}
}

func TestCSV(t *testing.T) {
	var b bytes.Buffer
	_ = Write(&b, "csv", recs)
	rows, err := csv.NewReader(&b).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[1][1] != "The Persuader" || !strings.Contains(strings.Join(rows[1], ","), "watch?v=a") {
		t.Fatalf("rows = %v", rows)
	}
}

func TestTableFitsWidth(t *testing.T) {
	long := []crate.Record{{
		Artist: "An Extremely Long Artist Name That Goes On And On", Title: "A Title That Is Also Far Too Long For Any Table",
		Label: "Some Label Name Here", Year: "1994", Price: "€ 17.5", Source: "hardwax:this-week", Kind: "shop",
		Note: "A long shop note that should be cut", YouTubeSearchURL: "https://y",
	}, {Artist: "日本の アーティスト", Title: "Short", Source: "oye:new", Kind: "shop"}}
	var b bytes.Buffer
	if err := WriteOpts(&b, "table", long, Options{Width: 90}); err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimRight(b.String(), "\n"), "\n") {
		if w := textWidth(line); w > 90 {
			t.Errorf("line is %d columns wide:\n%s", w, line)
		}
	}
	if !strings.Contains(b.String(), "€17.50") || !strings.Contains(b.String(), "…") {
		t.Errorf("want tidy price and truncation:\n%s", b.String())
	}
	// Colour and links add escape codes but no visible width.
	var c bytes.Buffer
	WriteOpts(&c, "table", long, Options{Width: 90, Color: true, Links: true})
	if !strings.Contains(c.String(), "\x1b]8;;https://y") {
		t.Error("missing hyperlink")
	}
}
