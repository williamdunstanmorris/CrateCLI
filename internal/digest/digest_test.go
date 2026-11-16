package digest

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/williamdunstanmorris/cratecli/internal/crate"
)

const sample = `
timezone: Europe/Berlin
from: "Crate <music@example.com>"
recipients:
  me: me@example.com
digests:
  - name: tuesday
    title: Berlin shops
    days: [tue]
    at: "07:30"
    to: [me, friend@example.com]
    sections:
      - title: Hard Wax
        dig: { from: [hardwax], limit: all, fast: true }
      - title: Pop
        dig: { search: { genre: Pop, year: "2025-2026", sort: want }, limit: 25 }
`

func load(t *testing.T, yaml string) (*File, error) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "digests.yaml")
	if err := os.WriteFile(p, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	return Load(p)
}

func TestLoad(t *testing.T) {
	f, err := load(t, sample)
	if err != nil {
		t.Fatal(err)
	}
	d, err := f.Find("Tuesday")
	if err != nil {
		t.Fatal(err)
	}
	if !d.Sections[0].Dig.Limit.All || !d.Sections[0].Dig.Fast || d.Sections[1].Dig.Limit.N != 25 {
		t.Errorf("sections = %+v", d.Sections)
	}
	if d.Sections[1].Dig.Search == nil || d.Sections[1].Dig.Search.Genre != "Pop" {
		t.Errorf("search = %+v", d.Sections[1].Dig.Search)
	}
	to, err := f.Addresses(d, nil)
	if err != nil || len(to) != 2 || to[0] != "me@example.com" {
		t.Errorf("addresses = %v %v", to, err)
	}

	loc, _ := f.Location()
	tue := time.Date(2026, 11, 17, 9, 0, 0, 0, loc)
	at, ok := d.ScheduledOn(tue, loc)
	if !ok || at.Hour() != 7 || at.Minute() != 30 {
		t.Errorf("scheduled = %v %v", at, ok)
	}
	if _, ok := d.ScheduledOn(tue.AddDate(0, 0, 1), loc); ok {
		t.Error("scheduled on Wednesday")
	}
}

func TestLoadErrors(t *testing.T) {
	for name, bad := range map[string]string{
		"typo key":   strings.Replace(sample, "sections:", "sectoins:", 1),
		"bad day":    strings.Replace(sample, "[tue]", "[tuesdayy]", 1),
		"bad time":   strings.Replace(sample, `"07:30"`, `"7.30am"`, 1),
		"bad person": strings.Replace(sample, "[me, friend@example.com]", "[nobody]", 1),
		"bad limit":  strings.Replace(sample, "limit: 25", "limit: lots", 1),
	} {
		if _, err := load(t, bad); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}

func TestRender(t *testing.T) {
	is := &Issue{
		Digest: Digest{Name: "tuesday", Title: "Berlin shops", MaxPerSection: 2},
		Date:   time.Date(2026, 11, 17, 7, 30, 0, 0, time.UTC),
		Sections: []IssueSection{
			{Title: "Hard Wax", Records: []crate.Record{
				{Artist: "Chez Damier", Title: "Can You Feel It", Label: "Balance", Note: "Detroit House <classic>",
					Source: "hardwax:this-week", SourceURL: "https://hardwax.com/x", YouTubeSearchURL: "https://y/s",
					Videos: []crate.Link{{URL: "https://y/v"}}},
				{Artist: "Kerrie", Title: "Pressure Signal", YouTubeSearchURL: "https://y/k"},
				{Artist: "Third", Title: "Cut", YouTubeSearchURL: "https://y/t"},
			}},
			{Title: "OYE", Err: errors.New("timeout")},
		},
	}
	m, err := Render(is, "Crate <music@example.com>", "laptop")
	if err != nil {
		t.Fatal(err)
	}
	if m.Subject != "Crate · Tuesday · Berlin shops: 3 new records" {
		t.Errorf("subject = %q", m.Subject)
	}
	for _, want := range []string{"Chez Damier", `href="https://y/v"`, "▶ Watch", "⌕ Search YouTube",
		"Detroit House &lt;classic&gt;", "+ 1 more in the attached CSV", "Couldn&#39;t fetch this section: timeout"} {
		if !strings.Contains(m.HTML, want) {
			t.Errorf("HTML missing %q", want)
		}
	}
	if strings.Contains(m.HTML, "Third") {
		t.Error("row 3 should be cut from the HTML")
	}
	if len(m.Attachments) != 1 || !strings.Contains(string(m.Attachments[0].Data), "Third") {
		t.Error("CSV attachment should hold every record")
	}
	if !strings.Contains(m.Text, "1. Chez Damier – Can You Feel It") || !strings.Contains(m.Text, "https://y/v") {
		t.Errorf("text:\n%s", m.Text)
	}
}
