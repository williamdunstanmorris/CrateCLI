package sources

import (
	"os"
	"strconv"
	"strings"
	"testing"
)

func TestParseYoyakuWP(t *testing.T) {
	b, err := os.ReadFile("testdata/yoyaku_wp.json")
	if err != nil {
		t.Fatal(err)
	}
	recs, ids, err := parseYoyakuWP(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 2 || len(ids) != 2 {
		t.Fatalf("got %d records, %d ids", len(recs), len(ids))
	}
	r := recs[0]
	if r.Artist != "Aja & Mami Konishi" || r.Title != "Shimo Naps" || r.Label != "Chapelle XIV Music" {
		t.Errorf("record = %+v", r)
	}
	if len(r.Tags) != 2 || r.Tags[0] != "Ambient" || r.Country != "Japan" {
		t.Errorf("tags/country = %v %q", r.Tags, r.Country)
	}

	store := []byte(`[{"id":` + strconv.Itoa(ids[1]) + `,"sku":"S2S001","prices":{"price":"2100","currency_code":"EUR","currency_minor_unit":2}}]`)
	if err := addYoyakuStore(store, recs, ids); err != nil {
		t.Fatal(err)
	}
	if recs[1].CatNo != "S2S001" || recs[1].Price != "€ 21.00" || recs[0].Price != "" {
		t.Errorf("store data = %+v / %+v", recs[0], recs[1])
	}
}

func TestParseShopify(t *testing.T) {
	b, err := os.ReadFile("testdata/rubadub.json")
	if err != nil {
		t.Fatal(err)
	}
	recs, err := parseShopify(b, "https://rubadub.co.uk", "£")
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 2 {
		t.Fatalf("want 2 records (gear skipped), got %d", len(recs))
	}
	r := recs[0]
	if r.Artist != "Grand River" || r.Title != "Temporary" || r.Label != "Editions Mego" ||
		r.CatNo != "eMego320" || r.Price != "£ 25.00" || r.Format != "Vinyl" {
		t.Errorf("record = %+v", r)
	}
	if !strings.HasPrefix(r.Note, "Line one line two") {
		t.Errorf("note = %q (words glued at <br>?)", r.Note)
	}
	if r.SourceURL != "https://rubadub.co.uk/products/temporary" {
		t.Errorf("url = %s", r.SourceURL)
	}
}

func TestJoinArtists(t *testing.T) {
	for _, c := range []struct {
		in   []string
		want string
	}{{nil, ""}, {[]string{"A"}, "A"}, {[]string{"A", "B"}, "A & B"}, {[]string{"A", "B", "C"}, "A, B & C"}} {
		if got := joinArtists(c.in); got != c.want {
			t.Errorf("joinArtists(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}
