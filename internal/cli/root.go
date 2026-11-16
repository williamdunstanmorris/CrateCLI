// Package cli wires up the cratecli commands.
package cli

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/williamdunstanmorris/cratecli/internal/browser"
	"github.com/williamdunstanmorris/cratecli/internal/config"
	"github.com/williamdunstanmorris/cratecli/internal/core"
	"github.com/williamdunstanmorris/cratecli/internal/crate"
	"github.com/williamdunstanmorris/cratecli/internal/discogs"
	"github.com/williamdunstanmorris/cratecli/internal/library"
	"github.com/williamdunstanmorris/cratecli/internal/output"
)

// errWriter receives progress and warnings, keeping stdout clean for data.
var errWriter io.Writer = os.Stderr

// Version is set at build time via -ldflags "-X .../cli.Version=...".
var Version = "dev"

// globalFlags apply to every dig command.
type globalFlags struct {
	format      string
	limit       core.Limit
	fast        bool
	seen        bool
	videos      int
	videoSource string
	open        bool
	openMax     int
	noCache     bool
	quiet       bool
	color       string
}

type app struct {
	cfg   config.Config
	flags globalFlags

	lib    *library.Library
	libErr error
	svc    *core.Service
}

// Execute runs the root command.
func Execute() int {
	a := &app{}
	root := &cobra.Command{
		Use:   "cratecli",
		Short: "Dig for records on Discogs and listen on YouTube",
		Long: `cratecli digs through Discogs (searches, record shops' inventories,
other people's wantlists) and gives you YouTube links to listen.

Data goes to stdout and progress/warnings go to stderr, so it pipes cleanly:
  cratecli search --style "Dub Techno" --random -f urls | head -3

Take recommendations from shops, magazines and radio:
  cratecli sources                   # what's available
  cratecli from hardwax              # Hard Wax's arrivals this week
  cratecli from oye bestsellers --style house --open

Find what to dig for:
  cratecli styles house              # look up style names
  cratecli years --style House       # when was it big?
  cratecli search --style House --year 90s --random --open`,
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       Version,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			a.cfg = cfg
			return nil
		},
	}

	pf := root.PersistentFlags()
	pf.StringVarP(&a.flags.format, "format", "f", "table", "output format: "+strings.Join(output.Formats, ", "))
	a.flags.limit = core.DefaultLimit
	pf.VarP(&a.flags.limit, "limit", "n", `number of records to return, or "all"`)
	pf.BoolVar(&a.flags.fast, "fast", false, "skip Discogs lookups: much faster, YouTube search links only")
	pf.BoolVar(&a.flags.seen, "seen", false, "include records you've already seen (from and --random digs hide them)")
	pf.IntVar(&a.flags.videos, "videos", 3, "max video links per record")
	pf.StringVar(&a.flags.videoSource, "video-source", "auto",
		"where to find videos: auto (Discogs, then YouTube API), discogs, youtube, none")
	pf.BoolVarP(&a.flags.open, "open", "o", false, "open each record's best link in a browser tab")
	pf.IntVar(&a.flags.openMax, "open-max", 10, "safety cap on tabs opened by --open")
	pf.BoolVar(&a.flags.noCache, "no-cache", false, "ignore the on-disk release cache")
	pf.BoolVarP(&a.flags.quiet, "quiet", "q", false, "suppress progress messages")
	pf.StringVar(&a.flags.color, "color", "auto", "colour and clickable links: auto, always, never")

	root.AddCommand(
		a.searchCmd(),
		a.sellerCmd(),
		a.wantsCmd(),
		a.releaseCmd(),
		a.artistCmd(),
		a.stylesCmd(),
		a.genresCmd(),
		a.yearsCmd(),
		a.fromCmd(),
		a.sourcesCmd(),
		a.configCmd(),
		a.seenCmd(),
		a.lastCmd(),
		a.saveCmd(),
		a.savedCmd(),
		a.unsaveCmd(),
		a.openCmd(),
		a.showCmd(),
		a.digestCmd(),
		a.youtubeCmd(),
		a.syncCmd(),
		a.serveCmd(),
	)

	err := root.Execute()
	if a.lib != nil {
		a.lib.Close()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	return 0
}

func (a *app) logf(format string, args ...any) {
	if !a.flags.quiet {
		fmt.Fprintf(os.Stderr, format+"\n", args...)
	}
}

// core returns the service layer, built on first use once flags and
// config are known. Every command goes through it.
func (a *app) core() *core.Service {
	if a.svc == nil {
		a.svc = core.New(a.cfg, core.Options{Library: a.library(), Log: a.logf, NoCache: a.flags.noCache})
	}
	return a.svc
}

func (a *app) discogs() *discogs.Client { return a.core().Discogs }

// runOpts tweaks the shared pipeline for commands that fetch their own
// records (seller, wants, release).
type runOpts struct {
	// track hides records already seen and marks the printed ones as seen.
	track bool
	// match finds Discogs releases for records that lack an ID.
	match bool
}

// run fetches records, enriches them, prints them and optionally opens them.
func (a *app) run(cmd *cobra.Command, _ *discogs.Client, fetch core.Fetcher) error {
	return a.runWith(cmd, nil, fetch, runOpts{})
}

func (a *app) runWith(cmd *cobra.Command, _ *discogs.Client, fetch core.Fetcher, o runOpts) error {
	src, err := crate.ParseVideoSource(a.flags.videoSource)
	if err != nil {
		return err
	}
	recs, _, err := a.core().Run(cmd.Context(), fetch, core.RunOptions{
		Limit:       a.flags.limit,
		Track:       o.track,
		IncludeSeen: a.flags.seen,
		Fast:        a.flags.fast,
		Match:       o.match,
		VideoSource: src,
		MaxVideos:   a.flags.videos,
	})
	if err != nil || len(recs) == 0 {
		return err
	}
	return a.emitDig(cmd, recs, o.track)
}

// dig runs a DigRequest built from flags and prints the result.
func (a *app) dig(cmd *cobra.Command, req core.DigRequest) error {
	req.Limit = a.flags.limit
	req.IncludeSeen = req.IncludeSeen || a.flags.seen
	req.Fast = req.Fast || a.flags.fast
	req.VideoSource = a.flags.videoSource
	req.MaxVideos = a.flags.videos
	res, err := a.core().Dig(cmd.Context(), req)
	if err != nil || len(res.Records) == 0 {
		return err
	}
	return a.emitDig(cmd, res.Records, res.Track)
}

// enricher builds the Discogs/YouTube lookup pipeline (used by save).
func (a *app) enricher(src crate.VideoSource, match bool) (*crate.Enricher, error) {
	return a.core().Enricher(core.EnrichOptions{VideoSource: src, MaxVideos: a.flags.videos, Match: match})
}

// emit prints records, remembers them as the last dig (for `last` and
// `save`) and opens them in the browser if asked.
func (a *app) emit(cmd *cobra.Command, recs []crate.Record) error {
	return a.emitDig(cmd, recs, false)
}

// emitDig is emit that can also mark the records as seen.
func (a *app) emitDig(cmd *cobra.Command, recs []crate.Record, markSeen bool) error {
	if err := a.show(cmd, recs); err != nil {
		return err
	}
	if _, err := a.core().Record(cmd.Context(), commandLine(), recs, markSeen); err != nil {
		a.logf("warn: saving to library: %v", err)
	}
	return nil
}

// show prints records and opens them, without touching the library.
func (a *app) show(cmd *cobra.Command, recs []crate.Record) error {
	out := cmd.OutOrStdout()
	o, tty := a.termOptions(out)
	if err := output.WriteOpts(out, a.flags.format, recs, o); err != nil {
		return err
	}
	if tty && a.flags.format == "table" && len(recs) > 0 && !a.flags.quiet && cmd.Name() != "show" {
		ex := min(3, len(recs))
		hint := fmt.Sprintf("crate open %d · crate show %d · crate save %d · -f long for everything", ex, ex, ex)
		if o.Links {
			hint = "click a title to play · " + hint
		}
		if o.Color {
			hint = "\x1b[2m" + hint + "\x1b[0m"
		}
		fmt.Fprintln(os.Stderr, hint)
	}
	if a.flags.open {
		urls := make([]string, 0, len(recs))
		for _, r := range recs {
			urls = append(urls, r.BestURL())
		}
		if len(urls) > a.flags.openMax {
			a.logf("opening first %d of %d (raise with --open-max)", a.flags.openMax, len(urls))
			urls = urls[:a.flags.openMax]
		}
		return browser.OpenAll(urls)
	}
	return nil
}

// library opens the local library on first use. If it can't be opened the
// error is reported once and digs carry on without history.
func (a *app) library() *library.Library {
	if a.lib != nil || a.libErr != nil {
		return a.lib
	}
	path, err := library.DefaultPath()
	if err == nil {
		a.lib, err = library.Open(path)
	}
	if err != nil {
		a.libErr = err
		fmt.Fprintf(os.Stderr, "warn: library unavailable, seen history is off: %v\n", err)
	}
	return a.lib
}

// commandLine is how the current dig is labelled in history.
func commandLine() string {
	return strings.Join(os.Args[1:], " ")
}

// fetchSize is how many candidates to ask a source for (see core.FetchSize).
func (a *app) fetchSize(track bool) int {
	return core.FetchSize(a.flags.limit, track, a.flags.seen)
}

// termOptions decides colour, links and width for the table formats.
// Colour needs a terminal (or --color always) and respects NO_COLOR;
// links are only sent to terminals known to support OSC 8 hyperlinks.
func (a *app) termOptions(w io.Writer) (output.Options, bool) {
	var o output.Options
	f, ok := w.(*os.File)
	tty := ok && term.IsTerminal(int(f.Fd()))
	switch a.flags.color {
	case "always":
		o.Color = true
	case "never":
	default:
		o.Color = tty && os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb"
	}
	if o.Color {
		switch v := os.Getenv("CRATECLI_LINKS"); v {
		case "1", "true", "yes":
			o.Links = true
		case "0", "false", "no":
		default:
			o.Links = linkTerminal()
		}
	}
	if tty {
		if cols, _, err := term.GetSize(int(f.Fd())); err == nil && cols > 0 {
			o.Width = cols
		}
	}
	return o, tty
}

// linkTerminal reports whether the terminal is known to handle OSC 8
// hyperlinks; others might print the escape codes literally.
func linkTerminal() bool {
	switch os.Getenv("TERM_PROGRAM") {
	case "ghostty", "iTerm.app", "WezTerm", "vscode", "Hyper", "Tabby", "WarpTerminal", "rio":
		return true
	}
	t := os.Getenv("TERM")
	return strings.Contains(t, "kitty") || strings.Contains(t, "ghostty") || strings.Contains(t, "wezterm") ||
		os.Getenv("KITTY_WINDOW_ID") != "" || vteVersion() >= 5000
}

func vteVersion() int {
	n, _ := strconv.Atoi(os.Getenv("VTE_VERSION"))
	return n
}
