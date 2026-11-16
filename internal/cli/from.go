package cli

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/williamdunstanmorris/cratecli/internal/config"
	"github.com/williamdunstanmorris/cratecli/internal/core"
	"github.com/williamdunstanmorris/cratecli/internal/sources"
)

// registry is every source cratecli knows, keyed by name.
func (a *app) registry() map[string]sources.Source { return a.core().Sources }

func (a *app) fromCmd() *cobra.Command {
	var random, noMatch bool
	var episodes int
	var style, years string
	var tags []string
	cmd := &cobra.Command{
		Use:     "from <source> [section] [<source> [section]…]",
		Aliases: []string{"dig"},
		Short:   "Take recommendations from record shops, magazines and radio",
		Long: `Pull picks from curated sources: record shops' new arrivals and charts,
magazine reviews, radio tracklists. Each pick is matched to Discogs and
given YouTube links, just like a search.

Name one or more sources, each optionally followed by a section (or use
source:section). See them all with ` + "`cratecli sources`" + `. Groups you define
with ` + "`cratecli config group`" + ` work too, as @name.

--style and --year filter after the Discogs lookup, so they work on any
source. --style matches loosely ("house" matches Deep House, US House…)
against Discogs styles and the shop's own genre tags.`,
		Example: `  cratecli from hardwax                       # this week's arrivals
  cratecli from hardwax essentials --random -n 5 --open
  cratecli from oye bestsellers --style house
  cratecli from clone -f m3u > clone.m3u
  cratecli from pitchfork best
  cratecli from quietus album-of-the-week
  cratecli from nts the-trilogy-tapes         # latest episode's tracklist
  cratecli from nts the-trilogy-tapes -e 3    # the last three episodes
  cratecli from bbc late-junction             # Radio 3, latest tracklist
  cratecli from bbc gilles-peterson -e 2
  cratecli from bbc b006tp52                  # any BBC show by programme ID
  cratecli from bandcamp --tag dub-techno     # newest dub techno on Bandcamp
  cratecli from bandcamp top --tag deep-house --tag detroit
  cratecli from hardwax oye:new clone -n 12 --random
  cratecli from @berlin --year 90s`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if episodes < 1 {
				return fmt.Errorf("--episodes must be at least 1")
			}
			if noMatch {
				a.flags.fast = true
			}
			// Radio digs show whole tracklists unless -n is given.
			if !cmd.Flags().Changed("limit") && a.core().RadioOnly(args) {
				a.flags.limit.All = true
			}
			return a.dig(cmd, core.DigRequest{
				From: args, Style: style, Year: years, Tags: tags,
				Episodes: episodes, Random: random,
			})
		},
	}
	f := cmd.Flags()
	f.BoolVarP(&random, "random", "r", false, "shuffle the picks instead of taking the newest first")
	f.StringVar(&style, "style", "", `only keep records matching this style/genre, e.g. "house", "Dub Techno"`)
	f.StringVarP(&years, "year", "y", "", "only keep records from this year/range (1995, 90s, late 80s…)")
	f.StringSliceVarP(&tags, "tag", "t", nil, "tags for sources that take them (bandcamp); repeat or comma-separate")
	f.IntVarP(&episodes, "episodes", "e", 1, "radio (nts, bbc): how many recent episodes' tracklists to read")
	f.BoolVar(&noMatch, "no-match", false, "old name for --fast")
	_ = f.MarkHidden("no-match")
	return cmd
}

func (a *app) sourcesCmd() *cobra.Command {
	var kind string
	cmd := &cobra.Command{
		Use:   "sources [source]",
		Short: "List the shops, magazines and radio you can take recommendations from",
		Example: `  cratecli sources
  cratecli sources --kind shop
  cratecli sources hardwax`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			reg := a.registry()
			w := cmd.OutOrStdout()
			if len(args) == 1 {
				src, ok := reg[strings.ToLower(args[0])]
				if !ok {
					return fmt.Errorf("unknown source %q%s", args[0], core.SourceHint(reg, strings.ToLower(args[0])))
				}
				return describeSource(w, src.Info())
			}
			if a.flags.format == "json" {
				var infos []sources.Info
				for _, s := range sources.Sorted(reg) {
					infos = append(infos, s.Info())
				}
				return writeJSON(w, infos)
			}
			tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
			var current sources.Kind
			for _, s := range sources.Sorted(reg) {
				info := s.Info()
				if kind != "" && string(info.Kind) != strings.ToLower(kind) {
					continue
				}
				if info.Kind != current {
					if current != "" {
						fmt.Fprintln(tw)
					}
					current = info.Kind
					fmt.Fprintf(tw, "%s\n", strings.ToUpper(kindTitle(info.Kind)))
				}
				fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\n", info.Name, info.Location, sectionSummary(info), info.Via)
			}
			fmt.Fprintln(tw)
			if len(a.cfg.Groups) > 0 {
				fmt.Fprintln(tw, "YOUR GROUPS")
				names := make([]string, 0, len(a.cfg.Groups))
				for n := range a.cfg.Groups {
					names = append(names, n)
				}
				sort.Strings(names)
				for _, n := range names {
					fmt.Fprintf(tw, "  @%s\t%s\n", n, strings.Join(a.cfg.Groups[n], ", "))
				}
				fmt.Fprintln(tw)
			}
			fmt.Fprintln(tw, "* = default section.  Details: cratecli sources <name>")
			return tw.Flush()
		},
	}
	cmd.Flags().StringVar(&kind, "kind", "", "only this kind: shop, press, radio, mixed")
	return cmd
}

func kindTitle(k sources.Kind) string {
	switch k {
	case sources.KindShop:
		return "shops"
	case sources.KindPress:
		return "press"
	case sources.KindRadio:
		return "radio"
	}
	return string(k)
}

func sectionSummary(info sources.Info) string {
	if info.FreeSection {
		s := "<" + info.FreeSectionName + ">"
		if info.Episodes {
			s += "  -e <episodes>"
		}
		return s
	}
	var parts []string
	for i, s := range info.Sections {
		if i == 0 {
			parts = append(parts, s.Name+"*")
		} else {
			parts = append(parts, s.Name)
		}
		if i == 4 && len(info.Sections) > 6 {
			parts = append(parts, fmt.Sprintf("+%d more", len(info.Sections)-5))
			break
		}
	}
	s := strings.Join(parts, ", ")
	if info.AcceptSection != nil {
		s += ", <programme id>"
	}
	if info.Tags {
		s += "  --tag <tag>"
	}
	if info.Episodes {
		s += "  -e <episodes>"
	}
	return s
}

func describeSource(w interface{ Write([]byte) (int, error) }, info sources.Info) error {
	fmt.Fprintf(w, "%s  (%s", info.Name, info.Kind)
	if info.Location != "" {
		fmt.Fprintf(w, ", %s", info.Location)
	}
	fmt.Fprintf(w, ")\n%s\n%s\nvia: %s\n\n", info.Description, info.Homepage, info.Via)
	if info.FreeSection {
		fmt.Fprintf(w, "usage: cratecli from %s %s\n", info.Name, info.FreeSectionHelp)
		if info.Episodes {
			fmt.Fprintf(w, "latest episode by default; -e 3 reads the last three\n")
		}
		return nil
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "SECTIONS")
	for i, s := range info.Sections {
		star := ""
		if i == 0 {
			star = " (default)"
		}
		fmt.Fprintf(tw, "  %s\t%s%s\n", s.Name, s.Description, star)
	}
	if info.Tags {
		fmt.Fprintf(tw, "\ntakes --tag, e.g. cratecli from %s --tag dub-techno\n", info.Name)
	}
	if info.AcceptSection != nil {
		fmt.Fprintf(tw, "\nany other show works by its programme ID, the code in bbc.co.uk/programmes/<id>:\n  cratecli from %s b006tp52\n", info.Name)
	}
	if info.Episodes {
		fmt.Fprintf(tw, "\nlatest episode by default; -e 3 reads the last three\n")
	}
	return tw.Flush()
}

func (a *app) groupCmd() *cobra.Command {
	var del bool
	return &cobra.Command{
		Use:   "group <name> [source[:section]…]",
		Short: "Define a named mix of sources for `cratecli from @name`",
		Example: `  cratecli config group berlin hardwax oye oldschool-berlin
  cratecli config group sunday honestjons:reggae quietus pitchfork:best
  cratecli config group berlin --delete
  cratecli from @berlin -n 10 --random`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := strings.TrimPrefix(args[0], "@")
			c := a.cfg
			if c.Groups == nil {
				c.Groups = map[string][]string{}
			}
			switch {
			case del:
				delete(c.Groups, name)
			case len(args) == 1:
				g, ok := c.Groups[name]
				if !ok {
					return fmt.Errorf("no group @%s", name)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "@%s: %s\n", name, strings.Join(g, " "))
				return nil
			default:
				// Validate before saving.
				if _, err := a.core().ParsePicks(args[1:]); err != nil {
					return err
				}
				c.Groups[name] = args[1:]
			}
			p, err := config.Save(c)
			if err != nil {
				return err
			}
			if del {
				fmt.Fprintf(os.Stderr, "deleted @%s (%s)\n", name, p)
			} else {
				fmt.Fprintf(os.Stderr, "saved @%s = %s (%s)\n", name, strings.Join(args[1:], " "), p)
			}
			return nil
		},
	}
}
