package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/williamdunstanmorris/cratecli/internal/browser"
	"github.com/williamdunstanmorris/cratecli/internal/digest"
	"github.com/williamdunstanmorris/cratecli/internal/library"
	"github.com/williamdunstanmorris/cratecli/internal/mail"
)

func (a *app) digestCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "digest",
		Short: "Scheduled email digests: what to send, when, and to whom",
		Long: `Digests are emails of fresh records, one per weekday theme: press on
Monday, Berlin shops on Tuesday… They're defined in digests.yaml (create
it with ` + "`cratecli digest init`" + `). Sent records count as seen, so digests
and digs never repeat each other.

Set up sending once with ` + "`cratecli config mail`" + `, preview with
` + "`cratecli digest preview <name>`" + `, then let your Mac send them on schedule
with ` + "`cratecli digest local on`" + `.`,
	}
	cmd.AddCommand(a.digestInitCmd(), a.digestListCmd(), a.digestPreviewCmd(),
		a.digestSendCmd(), a.digestRunCmd(), a.digestRunsCmd(), a.digestLocalCmd())
	return cmd
}

// digests loads digests.yaml and checks every section against the sources.
func (a *app) digests() (*digest.File, string, error) {
	path, err := digest.DefaultPath()
	if err != nil {
		return nil, "", err
	}
	f, err := digest.Load(path)
	if err != nil {
		return nil, path, err
	}
	if err := f.Validate(a.core()); err != nil {
		return nil, path, fmt.Errorf("%s: %w", path, err)
	}
	return f, path, nil
}

// sender is the configured SMTP account, or an error saying how to set one.
func (a *app) sender() (mail.Sender, error) {
	m := a.cfg.Mail
	if !m.Configured() {
		return nil, fmt.Errorf("no mail server set up; run `cratecli config mail` (or preview with `cratecli digest preview`)")
	}
	return mail.SMTP{Host: m.Host, Port: m.Port, Username: m.Username, Password: m.Password}, nil
}

func (a *app) digestInitCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Write an example digests.yaml to edit",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			path, err := digest.DefaultPath()
			if err != nil {
				return err
			}
			if _, err := os.Stat(path); err == nil && !force {
				return fmt.Errorf("%s already exists (--force overwrites it)", path)
			} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				return err
			}
			if err := os.WriteFile(path, []byte(digest.Example), 0o600); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "wrote %s\nedit the from: and recipients:, then: cratecli digest list\n", path)
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "overwrite an existing file")
	return cmd
}

func (a *app) digestListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List your digests, their schedule and when each was last sent",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			f, path, err := a.digests()
			if err != nil {
				return err
			}
			if a.flags.format == "json" {
				return writeJSON(cmd.OutOrStdout(), f)
			}
			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "NAME\tTITLE\tWHEN\tTO\tSECTIONS\tLAST SENT")
			for _, d := range f.Digests {
				to, _ := f.Addresses(d, nil)
				last := "never"
				if lib := a.library(); lib != nil {
					if r, ok, _ := lib.LastSent(cmd.Context(), d.Name); ok {
						last = ago(r.FinishedAt)
					}
				}
				fmt.Fprintf(tw, "%s\t%s\t%s %s\t%s\t%d\t%s\n", d.Name, d.DisplayTitle(),
					strings.Join(d.Days, ","), orDefaultStr(d.At, "07:00"), strings.Join(to, ", "), len(d.Sections), last)
			}
			fmt.Fprintf(tw, "\nfrom %s · %s\n", f.From, path)
			if !a.cfg.Mail.Configured() {
				fmt.Fprintln(tw, "mail isn't set up yet: cratecli config mail")
			}
			return tw.Flush()
		},
	}
}

func (a *app) digestPreviewCmd() *cobra.Command {
	var open bool
	cmd := &cobra.Command{
		Use:   "preview <name>",
		Short: "Build a digest and save the email as HTML, without sending it",
		Long: `Runs the digest's digs and writes the email it would send to an HTML file
(and an .eml you can open in Mail). Nothing is sent and nothing is marked
seen, so you can preview as often as you like.`,
		Example: `  cratecli digest preview tuesday --open`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			f, _, err := a.digests()
			if err != nil {
				return err
			}
			d, err := f.Find(args[0])
			if err != nil {
				return err
			}
			r := digest.NewRunner(a.core(), f, nil)
			res, err := r.Send(cmd.Context(), d, digest.SendOptions{DryRun: true})
			if err != nil {
				return err
			}
			dir, err := previewDir()
			if err != nil {
				return err
			}
			m := res.Message
			to, _ := f.Addresses(d, nil)
			m.To = to[:1]
			var htmlPath string
			fs := mail.File{Dir: dir, Written: func(eml, html string) {
				htmlPath = html
				fmt.Fprintf(os.Stderr, "%s\n  %d records · %s\n  %s\n  %s\n", m.Subject, len(res.Issue.Records()), "nothing sent", html, eml)
			}}
			if err := fs.Send(cmd.Context(), m); err != nil {
				return err
			}
			if open {
				return browser.Open("file://" + htmlPath)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&open, "open", false, "open the preview in your browser")
	return cmd
}

func previewDir() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "cratecli", "previews"), nil
}

func (a *app) digestSendCmd() *cobra.Command {
	var to []string
	var empty bool
	var toDir string
	cmd := &cobra.Command{
		Use:   "send <name>",
		Short: "Build and send a digest now",
		Long: `Sends the digest now, whatever the schedule says. --to sends to other
recipients instead (names from digests.yaml or addresses), handy for a test.
Sent records count as seen.`,
		Example: `  cratecli digest send tuesday
  cratecli digest send tuesday --to you@example.com
  cratecli digest send tuesday --to-dir ./out    # write .eml files instead of sending`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			f, _, err := a.digests()
			if err != nil {
				return err
			}
			d, err := f.Find(args[0])
			if err != nil {
				return err
			}
			var s mail.Sender
			if toDir != "" {
				s = mail.File{Dir: toDir, Written: func(eml, _ string) { fmt.Fprintf(os.Stderr, "wrote %s\n", eml) }}
			} else if s, err = a.sender(); err != nil {
				return err
			}
			res, err := digest.NewRunner(a.core(), f, s).Send(cmd.Context(), d, digest.SendOptions{To: to, SendEmpty: empty})
			if err != nil {
				return err
			}
			reportRun(res.Run)
			return nil
		},
	}
	cmd.Flags().StringSliceVar(&to, "to", nil, "send to these recipients instead (names or addresses)")
	cmd.Flags().BoolVar(&empty, "empty", false, "send even when there's nothing new")
	cmd.Flags().StringVar(&toDir, "to-dir", "", "write the emails to this folder instead of sending")
	return cmd
}

func reportRun(r library.DigestRun) {
	switch r.Status {
	case library.RunSent:
		fmt.Fprintf(os.Stderr, "sent %s: %d records to %d recipient(s)\n", r.Digest, r.Records, r.Recipients)
	case library.RunSkipped:
		fmt.Fprintf(os.Stderr, "skipped %s: %s\n", r.Digest, r.Message)
	default:
		fmt.Fprintf(os.Stderr, "%s %s: %s\n", r.Status, r.Digest, r.Message)
	}
	if r.Status == library.RunSent && r.Message != "" {
		fmt.Fprintf(os.Stderr, "warn: %s\n", r.Message)
	}
}

func (a *app) digestRunCmd() *cobra.Command {
	var fallback bool
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Send every digest that's due now and hasn't been sent today",
		Long: `Checks the schedule and sends each digest whose time has passed today and
that hasn't gone out yet. This is what runs on a timer (see ` + "`digest local`" + `);
running it twice never sends anything twice.

With --fallback it first asks the server in digests.yaml (fallback.server)
whether it already sent each digest, and only sends what the server missed.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			f, _, err := a.digests()
			if err != nil {
				return err
			}
			s, err := a.sender()
			if err != nil {
				return err
			}
			r := digest.NewRunner(a.core(), f, s)
			due, err := r.Due(cmd.Context())
			if err != nil {
				return err
			}
			if fallback {
				due, err = a.notSentByServer(cmd, f, r, due)
				if err != nil {
					return err
				}
			}
			if len(due) == 0 {
				a.logf("nothing due")
				return nil
			}
			var failed []string
			for _, d := range due {
				res, err := r.Send(cmd.Context(), d, digest.SendOptions{})
				if err != nil {
					fmt.Fprintf(os.Stderr, "failed %s: %v\n", d.Name, err)
					failed = append(failed, d.Name)
					continue
				}
				reportRun(res.Run)
			}
			if len(failed) > 0 {
				return fmt.Errorf("%d digest(s) failed: %s", len(failed), strings.Join(failed, ", "))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&fallback, "fallback", false, "only send what the server (fallback.server) hasn't")
	return cmd
}

func (a *app) digestRunsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "runs [name]",
		Short: "Show recent digest sends, skips and failures",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			lib, err := a.needLibrary()
			if err != nil {
				return err
			}
			name := ""
			if len(args) == 1 {
				name = args[0]
			}
			runs, err := lib.Runs(cmd.Context(), name, "", 30)
			if err != nil {
				return err
			}
			if a.flags.format == "json" {
				return writeJSON(cmd.OutOrStdout(), runs)
			}
			if len(runs) == 0 {
				a.logf("no digest runs yet")
				return nil
			}
			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "WHEN\tDIGEST\tSTATUS\tRECORDS\tTO\tHOST\tNOTE")
			for _, r := range runs {
				fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%d\t%s\t%s\n", ago(r.FinishedAt), r.Digest, r.Status, r.Records, r.Recipients, r.Host, r.Message)
			}
			return tw.Flush()
		},
	}
}

func orDefaultStr(s, d string) string {
	if s == "" {
		return d
	}
	return s
}
