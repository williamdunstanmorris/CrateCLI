package cli

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/williamdunstanmorris/cratecli/internal/discogs"
	"github.com/williamdunstanmorris/cratecli/internal/styles"
)

func (a *app) genresCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "genres",
		Short: "List Discogs genres",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			w := cmd.OutOrStdout()
			switch a.flags.format {
			case "json":
				return writeJSON(w, styles.Genres())
			default:
				for _, g := range styles.Taxonomy {
					fmt.Fprintf(w, "%-24s %d styles\n", g.Name, len(g.Styles))
				}
			}
			return nil
		},
	}
}

func (a *app) stylesCmd() *cobra.Command {
	var genre, years string
	var counts bool
	cmd := &cobra.Command{
		Use:   "styles [filter]",
		Short: "List and look up Discogs styles (optionally with live release counts)",
		Long: `List Discogs styles grouped by genre. Give a word to filter, e.g. "house".

--counts asks Discogs how many releases each style has (one request per
style), optionally limited to a year range. Results are sorted by count, so
this answers "what was big in house in the 90s?".`,
		Example: `  cratecli styles
  cratecli styles house
  cratecli styles --genre Reggae
  cratecli styles house --counts --year 90s
  cratecli styles techno -f json`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			term := ""
			if len(args) == 1 {
				term = args[0]
			}
			if genre != "" {
				g, ok := styles.CanonicalGenre(genre)
				if !ok {
					return fmt.Errorf("unknown genre %q; one of: %s", genre, strings.Join(styles.Genres(), ", "))
				}
				genre = g
			}
			list := styles.Filter(term, genre)
			if len(list) == 0 {
				if sug := styles.Suggest(term); len(sug) > 0 {
					return fmt.Errorf("no styles match %q. Did you mean: %s?", term, strings.Join(sug, ", "))
				}
				return fmt.Errorf("no styles match %q", term)
			}

			var yr *styles.YearRange
			if years != "" {
				r, err := styles.ParseYears(years)
				if err != nil {
					return err
				}
				yr = &r
				counts = true
			}

			rows := make([]styleRow, len(list))
			for i, s := range list {
				rows[i] = styleRow{Style: s}
			}
			if counts {
				if err := a.countStyles(cmd.Context(), rows, genre, yr); err != nil {
					return err
				}
				sort.SliceStable(rows, func(i, j int) bool { return rows[i].Count > rows[j].Count })
			}
			return writeStyles(cmd.OutOrStdout(), a.flags.format, rows, counts, genre)
		},
	}
	f := cmd.Flags()
	f.StringVarP(&genre, "genre", "g", "", "only styles in this genre")
	f.BoolVarP(&counts, "counts", "c", false, "fetch live release counts from Discogs (1 request per style)")
	f.StringVarP(&years, "year", "y", "", "count releases in this year/range only (implies --counts)")
	return cmd
}

type styleRow struct {
	styles.Style
	Count int    `json:"count,omitempty"`
	Years string `json:"years,omitempty"`
}

func (a *app) countStyles(ctx context.Context, rows []styleRow, genre string, yr *styles.YearRange) error {
	dc := a.discogs()
	a.warnSlow(dc, len(rows))
	for i := range rows {
		p := discogs.SearchParams{Type: "release", Style: rows[i].Name, Genre: genre, PerPage: 1}
		if yr != nil {
			p.Year = yr.Param()
			rows[i].Years = yr.Param()
		}
		resp, err := dc.Search(ctx, p)
		if err != nil {
			return fmt.Errorf("count %s: %w", rows[i].Name, err)
		}
		rows[i].Count = resp.Pagination.Items
		a.progress(i+1, len(rows))
	}
	return nil
}

func writeStyles(w io.Writer, format string, rows []styleRow, counts bool, genre string) error {
	switch format {
	case "json":
		return writeJSON(w, rows)
	case "csv":
		cw := csv.NewWriter(w)
		_ = cw.Write([]string{"style", "genres", "count", "years"})
		for _, r := range rows {
			_ = cw.Write([]string{r.Name, strings.Join(r.Genres, "; "), countCell(r.Count), r.Years})
		}
		cw.Flush()
		return cw.Error()
	case "table", "":
	default:
		return fmt.Errorf("styles supports -f table, json or csv")
	}

	if counts {
		max := 0
		for _, r := range rows {
			max = maxInt(max, r.Count)
		}
		for _, r := range rows {
			fmt.Fprintf(w, "%-22s %9s  %s\n", r.Name, humanInt(r.Count), bar(r.Count, max, 30))
		}
		return nil
	}

	// Group by genre, in taxonomy order.
	byGenre := map[string][]string{}
	for _, r := range rows {
		for _, g := range r.Genres {
			if genre == "" || g == genre {
				byGenre[g] = append(byGenre[g], r.Name)
			}
		}
	}
	for _, g := range styles.Genres() {
		names := byGenre[g]
		if len(names) == 0 {
			continue
		}
		fmt.Fprintf(w, "%s (%d)\n", g, len(names))
		for _, n := range names {
			fmt.Fprintf(w, "  %s\n", n)
		}
		fmt.Fprintln(w)
	}
	return nil
}

func (a *app) yearsCmd() *cobra.Command {
	var sf searchFilters
	cmd := &cobra.Command{
		Use:   "years",
		Short: "Show how many releases match a search, per year or per decade",
		Long: `Draw a histogram of matching releases over time, so you can see when a
style peaked before digging into it.

Without --year it shows one bar per decade. With a range it shows one bar
per year. Each bar costs one Discogs request.`,
		Example: `  cratecli years --style House
  cratecli years --style House --year 90s
  cratecli years --label "Basic Channel"
  cratecli years --style Techno --year 1985-2000 -f csv`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, err := sf.resolve(a.logf)
			if err != nil {
				return err
			}
			var buckets []styles.YearRange
			if sf.req.Year == "" {
				buckets = styles.Decades(1940, time.Now().Year())
			} else {
				r, _ := styles.ParseYears(sf.req.Year)
				for y := r.From; y <= r.To; y++ {
					buckets = append(buckets, styles.YearRange{From: y, To: y})
				}
			}
			dc := a.discogs()
			a.warnSlow(dc, len(buckets))
			rows := make([]yearRow, len(buckets))
			for i, b := range buckets {
				q := p
				q.Year, q.PerPage, q.Sort, q.SortOrder = b.Param(), 1, "", ""
				resp, err := dc.Search(cmd.Context(), q)
				if err != nil {
					return err
				}
				rows[i] = yearRow{Years: b.Param(), Count: resp.Pagination.Items}
				if sf.req.Year == "" {
					rows[i].Years = strconv.Itoa(b.From) + "s"
				}
				a.progress(i+1, len(buckets))
			}
			return writeYears(cmd.OutOrStdout(), a.flags.format, rows)
		},
	}
	sf.bind(cmd.Flags(), false)
	return cmd
}

type yearRow struct {
	Years string `json:"years"`
	Count int    `json:"count"`
}

func writeYears(w io.Writer, format string, rows []yearRow) error {
	switch format {
	case "json":
		return writeJSON(w, rows)
	case "csv":
		cw := csv.NewWriter(w)
		_ = cw.Write([]string{"years", "count"})
		for _, r := range rows {
			_ = cw.Write([]string{r.Years, strconv.Itoa(r.Count)})
		}
		cw.Flush()
		return cw.Error()
	case "table", "":
	default:
		return fmt.Errorf("years supports -f table, json or csv")
	}
	max := 0
	for _, r := range rows {
		max = maxInt(max, r.Count)
	}
	for _, r := range rows {
		fmt.Fprintf(w, "%-9s %9s  %s\n", r.Years, humanInt(r.Count), bar(r.Count, max, 40))
	}
	return nil
}

// --- helpers ---

func (a *app) warnSlow(dc *discogs.Client, n int) {
	perMin := 25
	if dc.Authenticated() {
		perMin = 60
	}
	if secs := n * 60 / perMin; secs > 20 {
		a.logf("%d requests to Discogs, about %ds at %d req/min…", n, secs, perMin)
	}
}

func (a *app) progress(done, total int) {
	if a.flags.quiet || total < 5 || !isTerminal(os.Stderr) {
		return
	}
	fmt.Fprintf(errWriter, "\r  %d/%d", done, total)
	if done == total {
		fmt.Fprint(errWriter, "\r\033[K")
	}
}

func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

func bar(n, max, width int) string {
	if max <= 0 || n <= 0 {
		return ""
	}
	full := n * width * 8 / max
	blocks := []string{"", "▏", "▎", "▍", "▌", "▋", "▊", "▉"}
	s := strings.Repeat("█", full/8) + blocks[full%8]
	if s == "" {
		s = "▏"
	}
	return s
}

func humanInt(n int) string {
	if n < 0 {
		return "-"
	}
	s := strconv.Itoa(n)
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	return b.String()
}

func countCell(n int) string {
	if n < 0 {
		return ""
	}
	return strconv.Itoa(n)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
