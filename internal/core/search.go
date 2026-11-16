package core

import (
	"fmt"
	"slices"
	"strings"

	"github.com/williamdunstanmorris/cratecli/internal/discogs"
	"github.com/williamdunstanmorris/cratecli/internal/styles"
)

// SearchRequest is a Discogs database search, the same filters as the
// search bar on discogs.com. Field names match the CLI flags.
type SearchRequest struct {
	Query   string `json:"query,omitempty"`
	Artist  string `json:"artist,omitempty"`
	Title   string `json:"title,omitempty"`
	Label   string `json:"label,omitempty"`
	Genre   string `json:"genre,omitempty"`
	Style   string `json:"style,omitempty"`
	Country string `json:"country,omitempty"`
	Year    string `json:"year,omitempty"` // 1995, 1990-1999, 90s, "late 80s"
	Media   string `json:"media,omitempty"`
	CatNo   string `json:"catno,omitempty"`
	Track   string `json:"track,omitempty"`
	Credit  string `json:"credit,omitempty"`
	Type    string `json:"type,omitempty"` // release (default) or master
	Sort    string `json:"sort,omitempty"` // year, title, want, have
	Order   string `json:"order,omitempty"`
	Page    int    `json:"page,omitempty"`
}

// Empty reports whether no query or filter was given.
func (r SearchRequest) Empty() bool {
	r.Type, r.Sort, r.Order, r.Page = "", "", "", 0
	return r == SearchRequest{}
}

// Params validates the request and turns it into Discogs search
// parameters: style and genre get their proper capitalisation and the
// year expression is parsed. Unknown styles only warn, since the embedded
// style list may lag behind Discogs.
func (r SearchRequest) Params(warn func(string, ...any)) (discogs.SearchParams, error) {
	if warn == nil {
		warn = func(string, ...any) {}
	}
	p := discogs.SearchParams{
		Query: r.Query, Artist: r.Artist, ReleaseTitle: r.Title, Label: r.Label,
		Genre: r.Genre, Style: r.Style, Country: r.Country, Format: r.Media,
		CatNo: r.CatNo, Track: r.Track, Credit: r.Credit, Type: r.Type, Page: r.Page,
	}
	if p.Type == "" {
		p.Type = "release"
	}

	if p.Style != "" {
		if c, ok := styles.CanonicalStyle(p.Style); ok {
			p.Style = c
		} else if sug := styles.Suggest(p.Style); len(sug) > 0 {
			warn("warn: %q isn't a known Discogs style. Did you mean: %s?", p.Style, strings.Join(sug, ", "))
		} else {
			warn("warn: %q isn't a known Discogs style (see `cratecli styles`); Discogs may match it loosely", p.Style)
		}
	}
	if p.Genre != "" {
		if c, ok := styles.CanonicalGenre(p.Genre); ok {
			p.Genre = c
		} else {
			return p, fmt.Errorf("unknown genre %q; one of: %s", p.Genre, strings.Join(styles.Genres(), ", "))
		}
	}
	if r.Year != "" {
		yr, err := styles.ParseYears(r.Year)
		if err != nil {
			return p, err
		}
		p.Year = yr.Param()
	}

	if r.Sort != "" {
		s := strings.ToLower(r.Sort)
		if !slices.Contains(discogs.SearchSorts, s) {
			return p, fmt.Errorf("can't sort search by %q; use one of: %s", r.Sort, strings.Join(discogs.SearchSorts, ", "))
		}
		p.Sort = s
		p.SortOrder = "asc"
		if s == "want" || s == "have" {
			p.SortOrder = "desc"
		}
	}
	if r.Order != "" {
		o := strings.ToLower(r.Order)
		if o != "asc" && o != "desc" {
			return p, fmt.Errorf("--order must be asc or desc")
		}
		if p.Sort == "" {
			return p, fmt.Errorf("--order needs --sort")
		}
		p.SortOrder = o
	}
	return p, nil
}
