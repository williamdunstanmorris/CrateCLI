package styles

import "testing"

func TestParseYears(t *testing.T) {
	cases := map[string]string{
		"1995":         "1995",
		"1990-1999":    "1990-1999",
		"1999 - 1990":  "1990-1999",
		"1990..1999":   "1990-1999",
		"1990 to 1999": "1990-1999",
		"90s":          "1990-1999",
		"'90s":         "1990-1999",
		"1990s":        "1990-1999",
		"early 90s":    "1990-1993",
		"mid-90s":      "1994-1996",
		"Late 80s":     "1987-1989",
		"00s":          "2000-2009",
		"10s":          "2010-2019",
		"60s":          "1960-1969",
	}
	for in, want := range cases {
		got, err := ParseYears(in)
		if err != nil {
			t.Errorf("ParseYears(%q): %v", in, err)
			continue
		}
		if got.Param() != want {
			t.Errorf("ParseYears(%q) = %s, want %s", in, got.Param(), want)
		}
	}
	for _, bad := range []string{"", "nineties", "95s", "1850", "3000", "199"} {
		if _, err := ParseYears(bad); err == nil {
			t.Errorf("ParseYears(%q) should fail", bad)
		}
	}
}

func TestCanonical(t *testing.T) {
	if s, ok := CanonicalStyle("deep house"); !ok || s != "Deep House" {
		t.Errorf("got %q %v", s, ok)
	}
	if s, ok := CanonicalStyle("drum-n-bass"); !ok || s != "Drum n Bass" {
		t.Errorf("got %q %v", s, ok)
	}
	if g, ok := CanonicalGenre("soul"); !ok || g != "Funk / Soul" {
		t.Errorf("got %q %v", g, ok)
	}
	if g, ok := CanonicalGenre("electronic"); !ok || g != "Electronic" {
		t.Errorf("got %q %v", g, ok)
	}
}

func TestSuggest(t *testing.T) {
	got := Suggest("dub tecno")
	if len(got) == 0 || got[0] != "Dub Techno" {
		t.Errorf("Suggest(dub tecno) = %v", got)
	}
}

func TestFilterAndMultiGenre(t *testing.T) {
	hs := Filter("house", "")
	if len(hs) < 10 {
		t.Errorf("expected many house styles, got %d", len(hs))
	}
	for _, s := range All() {
		if s.Name == "Dub" && len(s.Genres) < 2 {
			t.Errorf("Dub should be in Electronic and Reggae, got %v", s.Genres)
		}
	}
	if len(Filter("", "Reggae")) == 0 {
		t.Error("no reggae styles")
	}
}
