package cli

import (
	"strings"

	"github.com/spf13/pflag"

	"github.com/williamdunstanmorris/cratecli/internal/core"
	"github.com/williamdunstanmorris/cratecli/internal/discogs"
)

// searchFilters binds the Discogs search flags shared by `search` and
// `years` to a core.SearchRequest.
type searchFilters struct {
	req core.SearchRequest
}

func (sf *searchFilters) bind(f *pflag.FlagSet, withSort bool) {
	r := &sf.req
	f.StringVar(&r.Artist, "artist", "", "artist name")
	f.StringVar(&r.Title, "title", "", "release title")
	f.StringVar(&r.Label, "label", "", "label name")
	f.StringVar(&r.Genre, "genre", "", "genre, e.g. Electronic, Jazz, Reggae (see `cratecli genres`)")
	f.StringVar(&r.Style, "style", "", `style, e.g. "Deep House" (see `+"`cratecli styles`"+`)`)
	f.StringVar(&r.Country, "country", "", "country of release")
	f.StringVarP(&r.Year, "year", "y", "", `year or range: 1995, 1990-1999, 90s, "late 80s"`)
	f.StringVar(&r.Media, "media", "", "format, e.g. Vinyl, 12\", LP, Cassette")
	f.StringVar(&r.CatNo, "catno", "", "catalogue number")
	f.StringVar(&r.Track, "track", "", "track title")
	f.StringVar(&r.Credit, "credit", "", "credited person (producer, engineer, …)")
	f.StringVar(&r.Type, "type", "release", "release or master")
	if withSort {
		f.StringVar(&r.Sort, "sort", "", "sort by: "+strings.Join(discogs.SearchSorts, ", ")+
			" (want/have = how many people want/own it)")
		f.StringVar(&r.Order, "order", "", "asc or desc (default: desc for want/have, asc otherwise)")
	}
}

// resolve validates the flags and returns ready-to-send search params.
func (sf *searchFilters) resolve(warn func(string, ...any)) (discogs.SearchParams, error) {
	return sf.req.Params(warn)
}
