package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/williamdunstanmorris/cratecli/internal/config"
	"github.com/williamdunstanmorris/cratecli/internal/core"
	"github.com/williamdunstanmorris/cratecli/internal/crate"
)

func (a *app) searchCmd() *cobra.Command {
	var sf searchFilters
	var random bool
	var page int
	cmd := &cobra.Command{
		Use:   "search [free text]",
		Short: "Search the Discogs database (like the search bar and filters on discogs.com)",
		Example: `  cratecli search "basic channel"
  cratecli search --style House --year 90s --random
  cratecli search --style House --year "late 80s" --sort want      # the classics
  cratecli search --style "Deep House" --year 1993 --sort have --order asc  # the obscurities
  cratecli search --style Techno --year 1990-1995 --sort year
  cratecli search --label "Chain Reaction" -n 5 --open
  cratecli search --artist Willmo --title Kleptocracker -f json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			req := sf.req
			req.Query = strings.Join(args, " ")
			req.Page = page
			return a.dig(cmd, core.DigRequest{Search: &req, Random: random})
		},
	}
	f := cmd.Flags()
	sf.bind(f, true)
	f.IntVar(&page, "page", 0, "result page (ignored with --random)")
	f.BoolVarP(&random, "random", "r", false, "jump to a random page of the results and shuffle")
	return cmd
}

func (a *app) sellerCmd() *cobra.Command {
	var o crate.SellerOptions
	cmd := &cobra.Command{
		Use:   "seller <discogs-username>",
		Short: "Dig a record shop's / seller's Discogs inventory (newest listings first)",
		Long: `Pull records from a Discogs seller's marketplace inventory.

Defaults to the most recently listed items. --random picks a random sort
order and page, so repeated runs surface different parts of a big shop.
Discogs only lets you page through the first 10,000 listings of someone
else's inventory per sort order, which --random works around.`,
		Example: `  cratecli seller decks.de
  cratecli seller decks.de --random -n 5 --open
  cratecli seller decks.de --sort price --order asc -f csv`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			o.Limit = a.fetchSize(o.Random)
			dc := a.discogs()
			return a.runWith(cmd, dc, func(ctx context.Context) ([]crate.Record, error) {
				return crate.FromSeller(ctx, dc, args[0], o)
			}, runOpts{track: o.Random})
		},
	}
	f := cmd.Flags()
	f.StringVar(&o.Sort, "sort", "", "listed, price, item, artist, label, catno, audio (default listed)")
	f.StringVar(&o.Order, "order", "", "asc or desc (default desc)")
	f.IntVar(&o.Page, "page", 0, "page (max 100; ignored with --random)")
	f.BoolVarP(&o.Random, "random", "r", false, "random sort, order and page, then shuffle")
	return cmd
}

func (a *app) wantsCmd() *cobra.Command {
	var random bool
	cmd := &cobra.Command{
		Use:   "wants <discogs-username>",
		Short: "Dig through another user's public wantlist",
		Example: `  cratecli wants someuser --random -n 5
  cratecli wants someuser -f m3u > theirtaste.m3u`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dc := a.discogs()
			return a.runWith(cmd, dc, func(ctx context.Context) ([]crate.Record, error) {
				return crate.FromWants(ctx, dc, args[0], a.fetchSize(random), random)
			}, runOpts{track: random})
		},
	}
	cmd.Flags().BoolVarP(&random, "random", "r", true, "random page and shuffle (use --random=false for most recent)")
	return cmd
}

func (a *app) releaseCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "release <release-id>",
		Short:   "Look up a single Discogs release and its videos",
		Example: "  cratecli release 1 --open",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.Atoi(strings.TrimPrefix(args[0], "r"))
			if err != nil || id <= 0 {
				return fmt.Errorf("release id must be a number, got %q", args[0])
			}
			dc := a.discogs()
			return a.run(cmd, dc, func(context.Context) ([]crate.Record, error) {
				return crate.FromRelease(id), nil
			})
		},
	}
}

func (a *app) configCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Show or set API credentials",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "show",
		Short: "Show the current configuration (secrets masked)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, _ := config.Path()
			fmt.Fprintf(cmd.OutOrStdout(), "config file:     %s\n", p)
			fmt.Fprintf(cmd.OutOrStdout(), "discogs token:   %s\n", mask(a.cfg.DiscogsToken))
			fmt.Fprintf(cmd.OutOrStdout(), "youtube api key: %s\n", mask(a.cfg.YouTubeAPIKey))
			fmt.Fprintf(cmd.OutOrStdout(), "user agent:      %s\n", a.cfg.UserAgent)
			if m := a.cfg.Mail; m.Configured() {
				fmt.Fprintf(cmd.OutOrStdout(), "mail server:     %s:%d as %s (password %s)\n", m.Host, m.Port, m.Username, mask(m.Password))
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "mail server:     (not set; cratecli config mail)\n")
			}
			fmt.Fprintf(cmd.OutOrStdout(), "youtube client:  %s\n", mask(a.cfg.YouTube.ClientID))
			for name, g := range a.cfg.Groups {
				fmt.Fprintf(cmd.OutOrStdout(), "group @%s:  %s\n", name, strings.Join(g, " "))
			}
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "set",
		Short: "Interactively save credentials to the config file",
		RunE: func(cmd *cobra.Command, _ []string) error {
			c := a.cfg
			in := bufio.NewReader(os.Stdin)
			ask := func(label, cur string) string {
				fmt.Fprintf(os.Stderr, "%s [%s] (enter to keep): ", label, mask(cur))
				line, _ := in.ReadString('\n')
				if line = strings.TrimSpace(line); line != "" {
					return line
				}
				return cur
			}
			c.DiscogsToken = ask("Discogs personal access token", c.DiscogsToken)
			c.YouTubeAPIKey = ask("YouTube Data API key (optional)", c.YouTubeAPIKey)
			p, err := config.Save(c)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "saved %s\n", p)
			return nil
		},
	})
	cmd.AddCommand(a.groupCmd(), a.mailConfigCmd())
	return cmd
}

func mask(s string) string {
	switch {
	case s == "":
		return "(not set)"
	case len(s) <= 8:
		return "****"
	}
	return s[:4] + "…" + s[len(s)-4:]
}

func (a *app) mailConfigCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mail",
		Short: "Set the SMTP account digests are sent from",
		Long: `Asks for an SMTP server, login and password, and saves them to the config
file (readable only by you).

For iCloud Mail, including a custom domain you set up in iCloud+:
  server   smtp.mail.me.com
  port     587
  username your Apple Account email
  password an app-specific password from account.apple.com
           (Sign-In and Security → App-Specific Passwords), not your real one

The digest's sender (digests.yaml "from:") can then be any address on your
custom domain. Environment variables CRATECLI_SMTP_HOST, _PORT, _USERNAME
and _PASSWORD override the file.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c := a.cfg
			in := bufio.NewReader(os.Stdin)
			ask := func(label, cur, def string) string {
				shown := cur
				if shown == "" {
					shown = def
				}
				fmt.Fprintf(os.Stderr, "%s [%s]: ", label, shown)
				line, _ := in.ReadString('\n')
				if line = strings.TrimSpace(line); line != "" {
					return line
				}
				return shown
			}
			c.Mail.Host = ask("SMTP server", c.Mail.Host, "smtp.mail.me.com")
			port := ask("Port", portString(c.Mail.Port), "587")
			p, err := strconv.Atoi(port)
			if err != nil {
				return fmt.Errorf("port %q isn't a number", port)
			}
			c.Mail.Port = p
			c.Mail.Username = ask("Username (for iCloud, your Apple Account email)", c.Mail.Username, "")
			fmt.Fprintf(os.Stderr, "Password (app-specific; typing is hidden) [%s]: ", mask(c.Mail.Password))
			pw, err := readSecret(in)
			if err != nil {
				return err
			}
			if pw != "" {
				c.Mail.Password = pw
			}
			path, err := config.Save(c)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "saved %s\ntest it with: cratecli digest send <name> --to you@example.com\n", path)
			return nil
		},
	}
}

func portString(p int) string {
	if p == 0 {
		return ""
	}
	return strconv.Itoa(p)
}

// readSecret reads a line without echoing it when stdin is a terminal.
func readSecret(in *bufio.Reader) (string, error) {
	if term.IsTerminal(int(os.Stdin.Fd())) {
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		return strings.TrimSpace(string(b)), err
	}
	line, _ := in.ReadString('\n')
	return strings.TrimSpace(line), nil
}
