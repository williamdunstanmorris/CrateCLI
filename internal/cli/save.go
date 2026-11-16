package cli

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/williamdunstanmorris/cratecli/internal/crate"
	"github.com/williamdunstanmorris/cratecli/internal/library"
	"github.com/williamdunstanmorris/cratecli/internal/youtube"
)

func (a *app) saveCmd() *cobra.Command {
	var note string
	var noLike bool
	cmd := &cobra.Command{
		Use:   "save <number…>",
		Short: "Keep records from your last dig, by their numbers",
		Long: `Adds records from the last dig (the numbers on the left of the table) to
your saved crate. See them with ` + "`cratecli saved`" + `.

Records from a --fast dig are looked up on Discogs as they're saved, so your
crate has the exact videos even when your browsing was quick. Add --fast
to skip that.

When you're signed in to YouTube (` + "`cratecli youtube login`" + `), the record's
video is also liked there (50 of the 10,000 daily API units). --no-like
skips that.`,
		Example: `  cratecli save 3
  cratecli save 3 7 12
  cratecli save 2-5 9
  cratecli save 4 --note "for Sunday's mix"`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			lib, err := a.needLibrary()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			d, recs, err := lib.LastDig(ctx)
			if errors.Is(err, library.ErrNoDigs) {
				return fmt.Errorf("nothing dug yet; try `cratecli from hardwax` first")
			}
			if err != nil {
				return err
			}
			nums, err := parseNumbers(args, len(recs))
			if err != nil {
				return fmt.Errorf("%w (last dig: `cratecli %s`, %d records)", err, d.Command, len(recs))
			}
			picked := make([]crate.Record, len(nums))
			for i, n := range nums {
				picked[i] = recs[n-1]
			}

			if !a.flags.fast {
				var need []int
				for i, r := range picked {
					if len(r.Videos) == 0 {
						need = append(need, i)
					}
				}
				if len(need) > 0 {
					if err := a.lookUp(cmd, picked, need); err != nil {
						return err
					}
				}
			}

			res, err := lib.Save(ctx, picked, note)
			if err != nil {
				return err
			}
			for i, r := range picked {
				what := map[library.SaveResult]string{
					library.Added: "saved", library.AlreadySaved: "already saved", library.NoteUpdated: "note updated",
				}[res[i]]
				extra := ""
				if len(r.Videos) > 0 {
					extra = fmt.Sprintf("  (%d videos)", len(r.Videos))
				}
				fmt.Fprintf(os.Stderr, "%-13s %2d. %s%s\n", what, nums[i], r.Query(), extra)
			}
			if !noLike {
				a.likeOnYouTube(cmd, picked, res)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&note, "note", "", "a note to keep with the record(s)")
	cmd.Flags().BoolVar(&noLike, "no-like", false, "don't like the video on YouTube")
	return cmd
}

// likeOnYouTube likes the video of each newly saved record, when signed
// in. Records with only a search link have no video to like. Problems
// only warn: the save itself already succeeded.
func (a *app) likeOnYouTube(cmd *cobra.Command, recs []crate.Record, res []library.SaveResult) {
	acct, err := a.youtubeAccount(cmd)
	if errors.Is(err, youtube.ErrNotSignedIn) {
		return
	}
	if err != nil {
		a.logf("warn: youtube: %v", err)
		return
	}
	lib, _ := a.needLibrary()
	for i, r := range recs {
		if res[i] != library.Added || len(r.Videos) == 0 {
			continue
		}
		id := youtube.VideoID(r.Videos[0].URL)
		if id == "" {
			continue
		}
		if err := acct.Like(cmd.Context(), id); err != nil {
			a.logf("warn: liking %s on YouTube: %v", r.Query(), err)
			if errors.Is(err, youtube.ErrQuotaExceeded) {
				return
			}
			continue
		}
		// Remember the like, so the next sync knows it came from here.
		if lib != nil {
			_, _ = lib.AddLike(cmd.Context(), library.Like{VideoID: id, Title: r.Videos[0].Title}, r)
		}
		fmt.Fprintf(os.Stderr, "liked on YouTube: %s\n", r.Query())
	}
}

// lookUp matches the records at idx on Discogs and finds their videos.
func (a *app) lookUp(cmd *cobra.Command, recs []crate.Record, idx []int) error {
	src, err := crate.ParseVideoSource(a.flags.videoSource)
	if err != nil {
		return err
	}
	e, err := a.enricher(src, true)
	if err != nil {
		return err
	}
	a.logf("looking up %d on Discogs…", len(idx))
	batch := make([]crate.Record, len(idx))
	for i, j := range idx {
		batch[i] = recs[j]
		batch[i].Videos = nil
	}
	e.Enrich(cmd.Context(), batch)
	for i, j := range idx {
		recs[j] = batch[i]
	}
	return nil
}

func (a *app) savedCmd() *cobra.Command {
	var source, origin string
	cmd := &cobra.Command{
		Use:   "saved",
		Short: "List your saved records, newest first",
		Example: `  cratecli saved
  cratecli saved --source hardwax
  cratecli saved --origin youtube      # liked on YouTube
  cratecli saved -f m3u | mpv --playlist=-
  cratecli saved -f csv > crate.csv`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			recs, err := a.savedList(cmd, source, origin)
			if err != nil {
				return err
			}
			if len(recs) == 0 {
				if source != "" || origin != "" {
					a.logf("nothing saved matching that filter")
				} else {
					a.logf("nothing saved yet: dig, then `cratecli save <number>`")
				}
				return nil
			}
			if !a.flags.limit.All && cmd.Flags().Changed("limit") {
				recs = recs[:a.flags.limit.Of(len(recs))]
			}
			a.logf("%d saved", len(recs))
			// Logged as a dig so `open 3` and `show 3` work on this list.
			return a.emit(cmd, recs)
		},
	}
	cmd.Flags().StringVar(&source, "source", "", "only records from this source, e.g. hardwax")
	cmd.Flags().StringVar(&origin, "origin", "", "only records saved this way: crate or youtube")
	return cmd
}

func (a *app) savedList(cmd *cobra.Command, source, origin string) ([]crate.Record, error) {
	lib, err := a.needLibrary()
	if err != nil {
		return nil, err
	}
	recs, err := lib.Saved(cmd.Context())
	if err != nil {
		return nil, err
	}
	if source != "" {
		var keep []crate.Record
		for _, r := range recs {
			if name, _, _ := strings.Cut(r.Source, ":"); strings.EqualFold(name, source) || strings.EqualFold(r.Source, source) {
				keep = append(keep, r)
			}
		}
		recs = keep
	}
	if origin != "" {
		var keep []crate.Record
		for _, r := range recs {
			if strings.EqualFold(r.Origin, origin) {
				keep = append(keep, r)
			}
		}
		recs = keep
	}
	for i := range recs {
		if t, err := time.Parse(time.RFC3339, recs[i].SavedAt); err == nil {
			recs[i].SavedAt = t.Local().Format("2006-01-02 15:04")
		}
	}
	return recs, nil
}

func (a *app) unsaveCmd() *cobra.Command {
	var source string
	cmd := &cobra.Command{
		Use:   "unsave <number…>",
		Short: "Remove records from your saved crate, by their numbers in `saved`",
		Example: `  cratecli saved
  cratecli unsave 2
  cratecli unsave 1-3 7`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			recs, err := a.savedList(cmd, source, "")
			if err != nil {
				return err
			}
			if len(recs) == 0 {
				return fmt.Errorf("nothing saved")
			}
			nums, err := parseNumbers(args, len(recs))
			if err != nil {
				return fmt.Errorf("%w (see `cratecli saved`)", err)
			}
			picked := make([]crate.Record, len(nums))
			for i, n := range nums {
				picked[i] = recs[n-1]
			}
			lib, _ := a.needLibrary()
			if _, err := lib.Unsave(cmd.Context(), picked); err != nil {
				return err
			}
			for i, r := range picked {
				fmt.Fprintf(os.Stderr, "unsaved %2d. %s\n", nums[i], r.Query())
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&source, "source", "", "number as in `saved --source …`")
	return cmd
}

// parseNumbers reads "3 7 12", "2-5" and "3,4" into distinct 1-based
// positions, in the order given.
func parseNumbers(args []string, max int) ([]int, error) {
	var out []int
	seen := map[int]bool{}
	add := func(n int) error {
		if n < 1 || n > max {
			return fmt.Errorf("no record %d; pick 1-%d", n, max)
		}
		if !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
		return nil
	}
	for _, arg := range args {
		for _, part := range strings.Split(arg, ",") {
			part = strings.TrimSpace(strings.TrimSuffix(part, "."))
			if part == "" {
				continue
			}
			lo, hi, isRange := strings.Cut(part, "-")
			a, err := strconv.Atoi(lo)
			if err != nil {
				return nil, fmt.Errorf("%q isn't a number", part)
			}
			b := a
			if isRange {
				if b, err = strconv.Atoi(hi); err != nil || b < a {
					return nil, fmt.Errorf("%q isn't a range like 2-5", part)
				}
			}
			for n := a; n <= b; n++ {
				if err := add(n); err != nil {
					return nil, err
				}
			}
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("give record numbers")
	}
	return out, nil
}
