package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/williamdunstanmorris/cratecli/internal/browser"
	"github.com/williamdunstanmorris/cratecli/internal/crate"
	"github.com/williamdunstanmorris/cratecli/internal/library"
)

func (a *app) needLibrary() (*library.Library, error) {
	if lib := a.library(); lib != nil {
		return lib, nil
	}
	return nil, a.libErr
}

func (a *app) seenCmd() *cobra.Command {
	var undo, clear, yes bool
	cmd := &cobra.Command{
		Use:   "seen",
		Short: "Your seen history: what `from` and --random digs hide next time",
		Long: `Every record printed by ` + "`from`" + ` or a --random dig counts as seen, and is
hidden from later digs so each run shows you something new. Add --seen to
any dig to include seen records again.

A record is recognised by artist and title (ignoring case, accents and
edition notes like "Remastered"), or by its Discogs release, so seeing it
at Hard Wax hides it at OYE too.`,
		Example: `  cratecli seen            # totals and recent digs
  cratecli seen --undo     # un-see the last dig (ran it but didn't look)
  cratecli seen --clear --yes`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			lib, err := a.needLibrary()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			w := cmd.OutOrStdout()
			switch {
			case undo:
				d, n, err := lib.UndoSeen(ctx)
				if errors.Is(err, library.ErrNoDigs) {
					return fmt.Errorf("no dig left to undo")
				}
				if err != nil {
					return err
				}
				fmt.Fprintf(os.Stderr, "un-saw %d records from `cratecli %s` (%s)\n", n, d.Command, ago(d.At))
				return nil
			case clear:
				if !yes {
					return fmt.Errorf("this forgets your whole seen history; add --yes to confirm")
				}
				n, err := lib.ClearSeen(ctx)
				if err != nil {
					return err
				}
				fmt.Fprintf(os.Stderr, "forgot %d seen records\n", n)
				return nil
			}

			s, err := lib.Stats(ctx)
			if err != nil {
				return err
			}
			if a.flags.format == "json" {
				return writeJSON(w, s)
			}
			fmt.Fprintf(w, "%d records seen, %d new today\n", s.Seen, s.Today)
			tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
			if len(s.BySource) > 0 {
				fmt.Fprintln(tw, "\nBY SOURCE")
				for _, c := range s.BySource {
					fmt.Fprintf(tw, "  %s\t%d\n", orDash(c.Source), c.Count)
				}
			}
			if len(s.RecentDig) > 0 {
				fmt.Fprintln(tw, "\nRECENT DIGS")
				for _, d := range s.RecentDig {
					mark := " "
					if d.Tracked {
						mark = "•"
					}
					fmt.Fprintf(tw, "  %s %s\t%d records\tcratecli %s\n", mark, ago(d.At), d.Count, d.Command)
				}
				fmt.Fprintln(tw, "\n  • = marked as seen")
			}
			return tw.Flush()
		},
	}
	f := cmd.Flags()
	f.BoolVar(&undo, "undo", false, "un-see the records first seen in the last dig")
	f.BoolVar(&clear, "clear", false, "forget the whole seen history")
	f.BoolVar(&yes, "yes", false, "confirm --clear")
	return cmd
}

func (a *app) lastCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "last",
		Short: "Show your last dig again, with the same numbers",
		Long: `Prints the records from your most recent dig in the same order and
numbering, without fetching anything. Handy for finding a number to save,
or for re-exporting a dig in another format.`,
		Example: `  cratecli last
  cratecli last -f m3u | mpv --playlist=-
  cratecli last --open`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			lib, err := a.needLibrary()
			if err != nil {
				return err
			}
			d, recs, err := lib.LastDig(cmd.Context())
			if errors.Is(err, library.ErrNoDigs) {
				return fmt.Errorf("nothing dug yet; try `cratecli from hardwax`")
			}
			if err != nil {
				return err
			}
			a.logf("cratecli %s  (%s, %d records)", d.Command, ago(d.At), len(recs))
			return a.show(cmd, recs)
		},
	}
}

// ago formats a time as "5m ago", "3h ago" or a date.
func ago(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
	return t.Local().Format("2 Jan 2006")
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}

// lastPicks returns records from the last dig by their numbers.
func (a *app) lastPicks(cmd *cobra.Command, args []string) ([]crate.Record, error) {
	lib, err := a.needLibrary()
	if err != nil {
		return nil, err
	}
	d, recs, err := lib.LastDig(cmd.Context())
	if errors.Is(err, library.ErrNoDigs) {
		return nil, fmt.Errorf("nothing dug yet; try `cratecli from hardwax`")
	}
	if err != nil {
		return nil, err
	}
	nums, err := parseNumbers(args, len(recs))
	if err != nil {
		return nil, fmt.Errorf("%w (last dig: `cratecli %s`, %d records)", err, d.Command, len(recs))
	}
	out := make([]crate.Record, len(nums))
	for i, n := range nums {
		out[i] = recs[n-1]
	}
	return out, nil
}

func (a *app) openCmd() *cobra.Command {
	var shop, discogs bool
	cmd := &cobra.Command{
		Use:   "open <number…>",
		Short: "Open records from the last dig in the browser, by number",
		Long: `Opens each record's best link: its first video, else a full-listen page,
else a YouTube search. --shop opens the source's page instead (to buy it),
--discogs the Discogs page.`,
		Example: `  cratecli open 3
  cratecli open 1-5
  cratecli open 7 --shop`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			recs, err := a.lastPicks(cmd, args)
			if err != nil {
				return err
			}
			var urls []string
			for _, r := range recs {
				u := r.BestURL()
				switch {
				case shop && r.SourceURL != "":
					u = r.SourceURL
				case shop && r.ListingURL != "":
					u = r.ListingURL
				case discogs && r.DiscogsURL != "":
					u = r.DiscogsURL
				case shop || discogs:
					a.logf("%s has no such page; opening the video/search instead", r.Query())
				}
				urls = append(urls, u)
			}
			return browser.OpenAll(urls)
		},
	}
	cmd.Flags().BoolVar(&shop, "shop", false, "open the shop's page for the record")
	cmd.Flags().BoolVar(&discogs, "discogs", false, "open the Discogs page")
	return cmd
}

func (a *app) showCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <number…>",
		Short: "Show every detail of records from the last dig",
		Example: `  cratecli show 3
  cratecli show 2-4 -f json`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			recs, err := a.lastPicks(cmd, args)
			if err != nil {
				return err
			}
			if a.flags.format == "table" {
				a.flags.format = "long"
			}
			return a.show(cmd, recs)
		},
	}
}
