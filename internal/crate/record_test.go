package crate

import "testing"

func TestCleanArtist(t *testing.T) {
	cases := map[string]string{
		"Willmo":          "Willmo",
		"Willmo (2)":      "Willmo",
		"Basic Channel*":  "Basic Channel",
		" DJ Sprinkles  ": "DJ Sprinkles",
		"Moodymann (12)*": "Moodymann",
	}
	for in, want := range cases {
		if got := CleanArtist(in); got != want {
			t.Errorf("CleanArtist(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSplitTitle(t *testing.T) {
	a, ti := SplitTitle("Willmo - Kleptocracker")
	if a != "Willmo" || ti != "Kleptocracker" {
		t.Fatalf("got %q / %q", a, ti)
	}
	a, ti = SplitTitle("Rhythm & Sound - See Mi Yah - Remixes")
	if a != "Rhythm & Sound" || ti != "See Mi Yah - Remixes" {
		t.Fatalf("got %q / %q", a, ti)
	}
}

func TestQueryAndBestURL(t *testing.T) {
	r := Record{Artist: "Willmo (2)", Title: "Kleptocracker", YouTubeSearchURL: "s"}
	if q := r.Query(); q != "Willmo - Kleptocracker" {
		t.Errorf("Query = %q", q)
	}
	if r.BestURL() != "s" {
		t.Errorf("BestURL should fall back to search URL")
	}
	r.Videos = []Link{{URL: "v"}}
	if r.BestURL() != "v" {
		t.Errorf("BestURL should prefer first video")
	}
	if q := (Record{Artist: "Various", Title: "Comp"}).Query(); q != "Comp" {
		t.Errorf("Various query = %q", q)
	}
}

func TestRandomPage(t *testing.T) {
	for i := 0; i < 1000; i++ {
		if p := randomPage(406, 100); p < 1 || p > 100 {
			t.Fatalf("page %d outside ceiling", p)
		}
		if p := randomPage(0, 0); p != 1 {
			t.Fatalf("empty result set should give page 1, got %d", p)
		}
	}
}

func TestStripEdition(t *testing.T) {
	cases := map[string]string{
		"Hidden Emotion  (30th Anniversary Edition) (3x12inch)": "Hidden Emotion",
		"Drommen Drommerne Drommer (LP)":                        "Drommen Drommerne Drommer",
		"Can You Feel It (Deep Mix)":                            "Can You Feel It (Deep Mix)",
		`Body Wisdom (2x12")`:                                   "Body Wisdom",
	}
	for in, want := range cases {
		if got := stripEdition(in); got != want {
			t.Errorf("stripEdition(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFoldAndSimilarity(t *testing.T) {
	if fold("Pépé Bradock & The Grand Brûlé's Choir") != "pepe bradock and the grand brule s choir" {
		t.Errorf("fold = %q", fold("Pépé Bradock & The Grand Brûlé's Choir"))
	}
	if similarity("rhythm and sound", "rhythm and sound") != 1 {
		t.Error("equal strings should score 1")
	}
	if s := similarity("kerrie", "basic channel"); s != 0 {
		t.Errorf("unrelated = %v", s)
	}
}

func TestPrimaryArtist(t *testing.T) {
	cases := map[string]string{
		"Henrik Villard / Anders Hajem":               "Henrik Villard",
		"Funki Cadets featuring Mike Dunn & Wade Teo": "Funki Cadets",
		"Rhythm & Sound":                              "Rhythm",
		"Moodymann":                                   "Moodymann",
		"Daniel Iskren, The Boogie Room":              "Daniel Iskren",
	}
	for in, want := range cases {
		if got := primaryArtist(in); got != want {
			t.Errorf("primaryArtist(%q) = %q, want %q", in, got, want)
		}
	}
}
