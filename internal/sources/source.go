// Package sources holds the curated feeds cratecli can pull recommendations
// from: record shops, magazines, radio and mixed platforms.
//
// Each source returns plain crate.Records (artist, title, label, a note…);
// the crate package then matches them to Discogs and finds videos.
//
// Most sources are read from public web pages or feeds, not official APIs,
// so they can break when a site changes. Adapters keep requests to a
// minimum, cache pages briefly, identify themselves honestly and skip
// anything robots.txt disallows.
package sources

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/williamdunstanmorris/cratecli/internal/crate"
	"github.com/williamdunstanmorris/cratecli/internal/discogs"
)

// Kind groups sources in listings. It's a label, not a command.
type Kind string

const (
	KindShop  Kind = "shop"
	KindPress Kind = "press"
	KindRadio Kind = "radio"
	KindMixed Kind = "mixed"
)

// Kinds is the display order for `cratecli sources`.
var Kinds = []Kind{KindShop, KindPress, KindRadio, KindMixed}

// Section is one browsable list at a source, e.g. Hard Wax "this-week".
type Section struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// Info describes a source for `cratecli sources`.
type Info struct {
	Name        string `json:"name"`
	Kind        Kind   `json:"kind"`
	Location    string `json:"location,omitempty"`
	Description string `json:"description"`
	Homepage    string `json:"homepage,omitempty"`
	// Via says how data is fetched: "web page", "RSS", "Discogs", "API".
	Via      string    `json:"via"`
	Sections []Section `json:"sections,omitempty"` // first is the default
	// FreeSection means the section word is free-form (e.g. an NTS show
	// slug) rather than one of Sections.
	FreeSection     bool   `json:"free_section,omitempty"`
	FreeSectionName string `json:"free_section_name,omitempty"` // e.g. "show"
	FreeSectionHelp string `json:"free_section_help,omitempty"`
	// Tags means the source accepts --tag filters (Bandcamp).
	Tags bool `json:"tags,omitempty"`
	// AcceptSection, when set, accepts section words beyond Sections
	// (e.g. a BBC programme ID). Functions can't be JSON, hence "-".
	AcceptSection func(string) bool `json:"-"`
	// Episodes means the source reads radio episodes and takes --episodes.
	Episodes bool `json:"episodes,omitempty"`
}

// DefaultSection is the section used when none is given.
func (i Info) DefaultSection() string {
	if len(i.Sections) == 0 {
		return ""
	}
	return i.Sections[0].Name
}

// HasSection reports whether name is a valid section for the source.
func (i Info) HasSection(name string) bool {
	if i.FreeSection {
		return true
	}
	for _, s := range i.Sections {
		if s.Name == name {
			return true
		}
	}
	return i.AcceptSection != nil && i.AcceptSection(name)
}

// Request is what the user asked a source for.
type Request struct {
	Section string
	Tags    []string
	// Want is a hint for how many records the caller needs; sources may
	// return more (whole pages) or fewer.
	Want int
	// Episodes is how many recent episodes radio sources read (0 = 1).
	Episodes int
}

// Source is one curated feed.
type Source interface {
	Info() Info
	Fetch(ctx context.Context, req Request) ([]crate.Record, error)
}

// Env is shared state sources may need.
type Env struct {
	HTTP    *Fetcher
	Discogs *discogs.Client
	// Log reports non-fatal problems (may be nil).
	Log func(string, ...any)
}

// Registry returns every known source, keyed by name.
func Registry(env Env) map[string]Source {
	all := []Source{
		&hardWax{env},
		&oye{env},
		&clone{env},
		&honestJons{env},
		&rushHour{env},
		&yoyaku{env},
		&rubadub{env},
		newDiscogsShop(env, "oldschool-berlin", "oldschool-berlin", "Berlin",
			"Second-hand and new vinyl, a big Berlin Discogs seller"),
		newDiscogsShop(env, "decks", "decks.de", "Germany",
			"Huge electronic mail-order shop (decks.de)"),
		&pitchfork{env},
		&quietus{env},
		&nts{env},
		&bbc{env},
		&bandcamp{env},
	}
	m := make(map[string]Source, len(all))
	for _, s := range all {
		m[s.Info().Name] = s
	}
	return m
}

// Sorted returns sources ordered by kind, then name.
func Sorted(reg map[string]Source) []Source {
	out := make([]Source, 0, len(reg))
	for _, s := range reg {
		out = append(out, s)
	}
	rank := map[Kind]int{}
	for i, k := range Kinds {
		rank[k] = i
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i].Info(), out[j].Info()
		if a.Kind != b.Kind {
			return rank[a.Kind] < rank[b.Kind]
		}
		return a.Name < b.Name
	})
	return out
}

// tag stamps source metadata onto records.
func tag(recs []crate.Record, info Info, section string) []crate.Record {
	src := info.Name
	if section != "" {
		src += ":" + section
	}
	for i := range recs {
		recs[i].Source = src
		recs[i].Kind = string(info.Kind)
	}
	return recs
}

func sectionError(info Info, section string) error {
	names := make([]string, len(info.Sections))
	for i, s := range info.Sections {
		names[i] = s.Name
	}
	return fmt.Errorf("%s has no section %q; try: %s", info.Name, section, strings.Join(names, ", "))
}

// firstSentence trims long blurbs to roughly one sentence.
func firstSentence(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if i := strings.IndexAny(s, ".!?"); i > 0 && i < max {
		return s[:i+1]
	}
	if len(s) <= max {
		return s
	}
	cut := strings.LastIndex(s[:max], " ")
	if cut < max/2 {
		cut = max
	}
	return s[:cut] + "…"
}
