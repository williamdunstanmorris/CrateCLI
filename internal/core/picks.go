package core

import (
	"context"
	"fmt"
	"math/rand/v2"
	"sort"
	"strconv"
	"strings"

	"github.com/williamdunstanmorris/cratecli/internal/crate"
	"github.com/williamdunstanmorris/cratecli/internal/sources"
	"github.com/williamdunstanmorris/cratecli/internal/styles"
)

// Pick is one source and section, e.g. Hard Wax "this-week".
type Pick struct {
	Source  sources.Source
	Section string
}

// ParsePicks turns words like "hardwax essentials oye:new @berlin" into
// picks. A word that isn't a source is the previous source's section;
// "@name" expands a group from the config.
func (s *Service) ParsePicks(args []string) ([]Pick, error) {
	reg := s.Sources
	var tokens []string
	for _, arg := range args {
		if name, ok := strings.CutPrefix(arg, "@"); ok {
			group, found := s.Config.Groups[name]
			if !found {
				return nil, fmt.Errorf("no group @%s; create it with `cratecli config group %s <sources…>`", name, name)
			}
			tokens = append(tokens, group...)
			continue
		}
		tokens = append(tokens, arg)
	}

	var picks []Pick
	for _, tok := range tokens {
		name, section, _ := strings.Cut(strings.ToLower(tok), ":")
		if src, ok := reg[name]; ok {
			picks = append(picks, Pick{src, section})
			continue
		}
		// Not a source: treat it as the previous source's section.
		if len(picks) > 0 && picks[len(picks)-1].Section == "" {
			last := &picks[len(picks)-1]
			if last.Source.Info().HasSection(name) {
				last.Section = name
				continue
			}
			return nil, fmt.Errorf("%q is neither a source nor a section of %s%s", tok, last.Source.Info().Name, SectionHint(last.Source.Info()))
		}
		return nil, fmt.Errorf("unknown source %q%s", tok, SourceHint(reg, name))
	}
	if len(picks) == 0 {
		return nil, fmt.Errorf("name at least one source; see `cratecli sources`")
	}

	for i := range picks {
		info := picks[i].Source.Info()
		if picks[i].Section == "" {
			picks[i].Section = info.DefaultSection()
		}
		if info.FreeSection && picks[i].Section == "" {
			return nil, fmt.Errorf("%s needs %s, e.g. `cratecli from %s the-trilogy-tapes`", info.Name, info.FreeSectionHelp, info.Name)
		}
		if !info.HasSection(picks[i].Section) {
			return nil, fmt.Errorf("%s has no section %q%s", info.Name, picks[i].Section, SectionHint(info))
		}
	}
	return picks, nil
}

// SectionHint lists a source's sections for error messages.
func SectionHint(info sources.Info) string {
	if info.FreeSection {
		return " (expects " + info.FreeSectionHelp + ")"
	}
	names := make([]string, len(info.Sections))
	for i, s := range info.Sections {
		names[i] = s.Name
	}
	return "; sections: " + strings.Join(names, ", ")
}

// SourceHint suggests sources with names close to name.
func SourceHint(reg map[string]sources.Source, name string) string {
	var close []string
	for n := range reg {
		if strings.Contains(n, name) || strings.Contains(name, n) || styles.Distance(n, name) <= 2 {
			close = append(close, n)
		}
	}
	sort.Strings(close)
	if len(close) > 0 {
		return "; did you mean " + strings.Join(close, ", ") + "?"
	}
	return "; see `cratecli sources`"
}

// FetchOptions controls FetchPicks.
type FetchOptions struct {
	Want     int // records wanted in total (split between picks)
	All      bool
	Tags     []string
	Random   bool
	Episodes int
}

// FetchPicks fetches every pick, then interleaves them so a mix of sources
// alternates between them. A failing source is reported but doesn't sink
// the others.
func (s *Service) FetchPicks(ctx context.Context, picks []Pick, o FetchOptions) ([]crate.Record, error) {
	want := o.Want
	if len(picks) > 1 && !o.All {
		want = want/len(picks) + 1
	}
	var lists [][]crate.Record
	var errs []string
	for _, p := range picks {
		info := p.Source.Info()
		label := info.Name
		if p.Section != "" {
			label += " " + p.Section
		}
		s.logf("fetching %s…", label)
		recs, err := p.Source.Fetch(ctx, sources.Request{Section: p.Section, Tags: o.Tags, Want: want, Episodes: o.Episodes})
		if err != nil {
			errs = append(errs, err.Error())
			s.logf("warn: %v", err)
			continue
		}
		if o.Random {
			rand.Shuffle(len(recs), func(i, j int) { recs[i], recs[j] = recs[j], recs[i] })
		}
		lists = append(lists, recs)
	}
	if len(lists) == 0 {
		if len(errs) == 1 {
			return nil, fmt.Errorf("%s", errs[0])
		}
		return nil, fmt.Errorf("no source returned records: %s", strings.Join(errs, "; "))
	}

	seen := map[string]bool{}
	var out []crate.Record
	for i := 0; ; i++ {
		added := false
		for _, l := range lists {
			if i >= len(l) {
				continue
			}
			added = true
			key := strings.ToLower(crate.CleanArtist(l[i].Artist) + "|" + l[i].Title)
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, l[i])
		}
		if !added {
			break
		}
	}
	return out, nil
}

// MatchesStyle reports whether a record's Discogs styles/genres or the
// source's own tags loosely contain style ("house" matches "Deep House").
func MatchesStyle(r crate.Record, style string) bool {
	if style == "" {
		return true
	}
	want := strings.ToLower(style)
	for _, list := range [][]string{r.Styles, r.Genres, r.Tags} {
		for _, s := range list {
			if strings.Contains(strings.ToLower(s), want) {
				return true
			}
		}
	}
	return false
}

// MatchesYear reports whether a record's year falls in yr (nil = any).
func MatchesYear(r crate.Record, yr *styles.YearRange) bool {
	if yr == nil {
		return true
	}
	y, err := strconv.Atoi(r.Year)
	return err == nil && y >= yr.From && y <= yr.To
}
