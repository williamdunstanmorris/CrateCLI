package cli

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/williamdunstanmorris/cratecli/internal/cache"
	"github.com/williamdunstanmorris/cratecli/internal/crate"
	"github.com/williamdunstanmorris/cratecli/internal/styles"
	"github.com/williamdunstanmorris/cratecli/internal/youtube"
)

func (a *app) artistCmd() *cobra.Command {
	var id int
	var appearances, newest bool
	var years string
	cmd := &cobra.Command{
		Use:   "artist <name>",
		Short: "A random selection of tunes by an artist",
		Long: `Pick random tunes from an artist's catalogue on Discogs.

cratecli walks their releases (one entry per album, not per pressing),
pools the YouTube videos people have linked on Discogs, and picks from that
pool, at most two per release, so you hear across their whole catalogue.
If several artists share the name, you'll be asked to pick one with --id.`,
		Example: `  cratecli artist shigeto
  cratecli artist shigeto -n 5 --open
  cratecli artist "Theo Parrish" --year 90s
  cratecli artist moodymann --appearances      # include remixes and features
  cratecli artist "Rhythm & Sound" --newest -f m3u | mpv --playlist=-`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			name := strings.Join(args, " ")
			if name == "" && id == 0 {
				return fmt.Errorf("give an artist name (or --id)")
			}
			if a.flags.limit.All {
				return fmt.Errorf(`artist takes a number for -n, not "all"`)
			}
			o := crate.ArtistOptions{Appearances: appearances, Newest: newest, Limit: a.flags.limit.N, Log: a.logf}
			if years != "" {
				r, err := styles.ParseYears(years)
				if err != nil {
					return err
				}
				o.YearFrom, o.YearTo = r.From, r.To
			}
			if !a.flags.noCache {
				o.Cache = cache.New("releases", 30*24*time.Hour)
			}

			dc := a.discogs()
			if !dc.Authenticated() {
				a.logf("note: no DISCOGS_TOKEN set; using the slower anonymous rate limit (25 req/min)")
			}
			ctx := cmd.Context()
			artist := crate.ArtistCandidate{ID: id, Name: name}
			if id == 0 {
				var err error
				artist, err = crate.FindArtist(ctx, dc, name)
				var amb *crate.AmbiguousArtistError
				if errors.As(err, &amb) {
					return err
				} else if err != nil {
					return err
				}
			}
			a.logf("digging through %s (discogs.com/artist/%d)…", artist.Name, artist.ID)
			tunes, err := crate.FromArtist(ctx, dc, artist, o)
			if err != nil {
				return err
			}
			for i := range tunes {
				tunes[i].YouTubeSearchURL = youtube.SearchURL(tunes[i].Query())
			}
			return a.emit(cmd, tunes)
		},
	}
	f := cmd.Flags()
	f.IntVar(&id, "id", 0, "Discogs artist ID (skips the name lookup)")
	f.BoolVar(&appearances, "appearances", false, "include remixes, features and compilation appearances")
	f.StringVarP(&years, "year", "y", "", "only releases from this year/range (2010s, 2013, 2010-2015)")
	f.BoolVar(&newest, "newest", false, "newest releases first instead of random")
	return cmd
}
