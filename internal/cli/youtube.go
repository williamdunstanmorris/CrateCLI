package cli

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/williamdunstanmorris/cratecli/internal/browser"
	"github.com/williamdunstanmorris/cratecli/internal/config"
	"github.com/williamdunstanmorris/cratecli/internal/youtube"
	"github.com/williamdunstanmorris/cratecli/internal/ytsync"
)

const youtubeSetupHelp = `cratecli needs your own Google OAuth client (free, once):
  1. console.cloud.google.com → create a project → enable "YouTube Data API v3"
  2. Google Auth Platform → Audience: External, add yourself as a test user,
     then "Publish app" (In production). Unverified is fine for personal use;
     apps left in Testing are signed out every 7 days.
  3. Clients → Create client → "Desktop app" → copy the ID and secret`

func (a *app) youtubeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "youtube",
		Short: "Sign in to your YouTube account (for syncing and adding likes)",
		Long: `Connects cratecli to your own YouTube account so that:
  - cratecli sync youtube  pulls videos you like on YouTube into ` + "`saved`" + `
  - cratecli save 3        also likes record 3's video on YouTube

` + youtubeSetupHelp,
	}
	cmd.AddCommand(
		&cobra.Command{Use: "login", Short: "Sign in with Google in your browser", Args: cobra.NoArgs, RunE: a.youtubeLogin},
		&cobra.Command{Use: "status", Short: "Show which YouTube account is connected", Args: cobra.NoArgs, RunE: a.youtubeStatus},
		&cobra.Command{Use: "logout", Short: "Forget the saved sign-in", Args: cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				tf, err := tokenFile()
				if err != nil {
					return err
				}
				if err := tf.Delete(); err != nil {
					return err
				}
				fmt.Fprintln(os.Stderr, "signed out (to revoke access fully: myaccount.google.com/connections)")
				return nil
			}},
	)
	return cmd
}

func tokenFile() (youtube.TokenFile, error) {
	dir, err := config.Dir()
	if err != nil {
		return youtube.TokenFile{}, err
	}
	return youtube.DefaultTokenFile(dir), nil
}

func (a *app) youtubeLogin(cmd *cobra.Command, _ []string) error {
	c := a.cfg
	if c.YouTube.ClientID == "" || c.YouTube.ClientSecret == "" {
		fmt.Fprint(os.Stderr, youtubeSetupHelp+"\n\n")
		in := bufio.NewReader(os.Stdin)
		fmt.Fprint(os.Stderr, "Client ID: ")
		id, _ := in.ReadString('\n')
		fmt.Fprint(os.Stderr, "Client secret (hidden): ")
		secret, err := readSecret(in)
		if err != nil {
			return err
		}
		c.YouTube.ClientID, c.YouTube.ClientSecret = strings.TrimSpace(id), secret
		if c.YouTube.ClientID == "" || c.YouTube.ClientSecret == "" {
			return fmt.Errorf("need both a client ID and a secret")
		}
		if _, err := config.Save(c); err != nil {
			return err
		}
		a.cfg = c
	}
	cfg := youtube.OAuthConfig(c.YouTube.ClientID, c.YouTube.ClientSecret)
	fmt.Fprintln(os.Stderr, "opening Google sign-in in your browser…")
	tok, err := youtube.Login(cmd.Context(), cfg, func(u string) error {
		if err := browser.Open(u); err != nil {
			fmt.Fprintf(os.Stderr, "open this link to sign in:\n%s\n", u)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if tok.RefreshToken == "" {
		return fmt.Errorf("Google didn't return a refresh token; remove cratecli at myaccount.google.com/connections and try again")
	}
	tf, err := tokenFile()
	if err != nil {
		return err
	}
	if err := tf.Save(tok); err != nil {
		return err
	}
	acct, err := a.youtubeAccount(cmd)
	if err != nil {
		return err
	}
	name, err := acct.ChannelName(cmd.Context())
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "signed in as %s\nnext: cratecli sync youtube\n", name)
	return nil
}

// youtubeAccount is the signed-in account, or ErrNotSignedIn.
func (a *app) youtubeAccount(cmd *cobra.Command) (*youtube.Account, error) {
	if a.cfg.YouTube.ClientID == "" {
		return nil, youtube.ErrNotSignedIn
	}
	tf, err := tokenFile()
	if err != nil {
		return nil, err
	}
	ts, err := tf.TokenSource(cmd.Context(), youtube.OAuthConfig(a.cfg.YouTube.ClientID, a.cfg.YouTube.ClientSecret))
	if err != nil {
		return nil, err
	}
	return youtube.NewAccount(cmd.Context(), ts), nil
}

func (a *app) youtubeStatus(cmd *cobra.Command, _ []string) error {
	acct, err := a.youtubeAccount(cmd)
	if errors.Is(err, youtube.ErrNotSignedIn) {
		fmt.Fprintln(cmd.OutOrStdout(), "not signed in: cratecli youtube login")
		return nil
	}
	if err != nil {
		return err
	}
	name, err := acct.ChannelName(cmd.Context())
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "signed in as %s\n", name)
	if lib := a.library(); lib != nil {
		if n, err := lib.LikeCount(cmd.Context()); err == nil {
			fmt.Fprintf(cmd.OutOrStdout(), "%d likes synced (cratecli saved --origin youtube)\n", n)
		}
	}
	return nil
}

func (a *app) syncCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Sync your saved records with other services",
	}
	var full, dry bool
	yt := &cobra.Command{
		Use:   "youtube",
		Short: "Pull videos you liked on YouTube into your saved records",
		Long: `Every video you've liked on YouTube becomes a saved record (origin
"youtube"), so ` + "`cratecli saved`" + ` is the full list of what you like, wherever you
liked it. Videos already saved through cratecli aren't duplicated.

Normally only new likes are read (1 quota unit per 50). --full re-reads
all of them and notices ones you've unliked; their records stay saved.`,
		Example: `  cratecli sync youtube
  cratecli sync youtube --full
  cratecli saved --origin youtube`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			acct, err := a.youtubeAccount(cmd)
			if err != nil {
				return err
			}
			lib, err := a.needLibrary()
			if err != nil {
				return err
			}
			res, err := ytsync.Sync(cmd.Context(), acct, lib, ytsync.Options{Full: full, DryRun: dry, Log: a.logf})
			if err != nil {
				return err
			}
			if a.flags.format == "json" {
				return writeJSON(cmd.OutOrStdout(), res)
			}
			verb := "synced"
			if dry {
				verb = "would sync"
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s %d new likes (%d newly saved) · %d checked · %d likes on YouTube\n",
				verb, res.New, res.NewSaved, res.Checked, res.Total)
			if res.Unliked > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "%d unliked on YouTube since (still saved)\n", res.Unliked)
			}
			for i, t := range res.NewTitles {
				if i == 15 {
					fmt.Fprintf(cmd.OutOrStdout(), "  … and %d more\n", len(res.NewTitles)-15)
					break
				}
				fmt.Fprintf(cmd.OutOrStdout(), "  + %s\n", t)
			}
			return nil
		},
	}
	yt.Flags().BoolVar(&full, "full", false, "read every like and notice unlikes")
	yt.Flags().BoolVar(&dry, "dry-run", false, "show what would be synced without saving")
	cmd.AddCommand(yt)
	return cmd
}
