package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/spf13/cobra"

	"github.com/williamdunstanmorris/cratecli/internal/digest"
)

// The Mac fallback is a launchd agent: macOS's built-in scheduler, which
// also runs jobs it missed while the laptop was asleep once it wakes.

const launchdLabel = "com.cratecli.digest"

func launchdPlistPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", launchdLabel+".plist"), nil
}

func (a *app) digestLocalCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "local",
		Short: "Switch scheduled sending on this Mac on or off",
		Long: `Your Mac can send the digests on schedule, using launchd (no Docker or
server needed). Three modes:

  on      this Mac sends every digest at its time
  auto    this Mac only sends what the server (fallback.server in
          digests.yaml) hasn't sent by its time plus the grace period
  off     this Mac sends nothing

It also retries every hour at :35, so a digest missed because the Mac was
asleep or offline goes out later the same day. Nothing is ever sent twice.
After editing digests.yaml, run ` + "`local on`" + ` (or auto) again to update times.`,
		Example: `  cratecli digest local on
  cratecli digest local auto
  cratecli digest local status
  cratecli digest local off`,
	}
	var print bool
	mode := func(auto bool) *cobra.Command {
		use, short := "on", "Send digests from this Mac on schedule"
		if auto {
			use, short = "auto", "Send from this Mac only what the server missed"
		}
		c := &cobra.Command{
			Use: use, Short: short, Args: cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error { return a.localOn(cmd, auto, print) },
		}
		c.Flags().BoolVar(&print, "print", false, "print the launchd file instead of installing it")
		return c
	}
	cmd.AddCommand(mode(false), mode(true),
		&cobra.Command{Use: "off", Short: "Stop sending digests from this Mac", Args: cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error { return localOff(true) }},
		&cobra.Command{Use: "status", Short: "Show whether this Mac sends digests", Args: cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error { return a.localStatus(cmd) }},
	)
	return cmd
}

type calendarEntry struct{ Weekday, Hour, Minute int }

var plistTmpl = template.Must(template.New("plist").Parse(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>{{.Label}}</string>
	<key>Comment</key>
	<string>cratecli digests, mode {{.Mode}}. Managed by: cratecli digest local</string>
	<key>ProgramArguments</key>
	<array>
{{- range .Args}}
		<string>{{.}}</string>
{{- end}}
	</array>
	<key>StartCalendarInterval</key>
	<array>
{{- range .Times}}
		<dict>
			<key>Weekday</key><integer>{{.Weekday}}</integer>
			<key>Hour</key><integer>{{.Hour}}</integer>
			<key>Minute</key><integer>{{.Minute}}</integer>
		</dict>
{{- end}}
		<dict>
			<key>Minute</key><integer>35</integer>
		</dict>
	</array>
	<key>StandardOutPath</key>
	<string>{{.Log}}</string>
	<key>StandardErrorPath</key>
	<string>{{.Log}}</string>
	<key>ProcessType</key>
	<string>Background</string>
</dict>
</plist>
`))

func (a *app) localOn(cmd *cobra.Command, auto, print bool) error {
	f, _, err := a.digests()
	if err != nil {
		return err
	}
	if !a.cfg.Mail.Configured() && !print {
		return fmt.Errorf("set up mail first: cratecli config mail")
	}
	grace := time.Duration(0)
	if auto {
		if f.Fallback.Server == "" {
			return fmt.Errorf("auto needs fallback.server in digests.yaml (the API that normally sends)")
		}
		grace = fallbackGrace(f)
	}
	// One launchd time per digest per weekday (plus grace in auto mode).
	var times []calendarEntry
	seen := map[calendarEntry]bool{}
	for _, d := range f.Digests {
		days, _ := d.Weekdays()
		h, m, _ := d.Clock()
		t := time.Date(2000, 1, 1, h, m, 0, 0, time.UTC).Add(grace)
		for _, wd := range days {
			e := calendarEntry{int(wd), t.Hour(), t.Minute()}
			if !seen[e] {
				seen[e] = true
				times = append(times, e)
			}
		}
	}
	bin, err := installedBinary()
	if err != nil {
		return err
	}
	home, _ := os.UserHomeDir()
	logPath := filepath.Join(home, "Library", "Logs", "cratecli", "digest.log")
	args := []string{bin, "digest", "run", "-q"}
	modeName := "on"
	if auto {
		args = append(args, "--fallback")
		modeName = "auto"
	}
	var buf bytes.Buffer
	if err := plistTmpl.Execute(&buf, map[string]any{
		"Label": launchdLabel, "Mode": modeName, "Args": args, "Times": times, "Log": logPath,
	}); err != nil {
		return err
	}
	if print {
		_, err := cmd.OutOrStdout().Write(buf.Bytes())
		return err
	}

	path, err := launchdPlistPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	_ = localOff(false) // replace any previous version
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		return err
	}
	if out, err := exec.Command("launchctl", "bootstrap", launchdDomain(), path).CombinedOutput(); err != nil {
		return fmt.Errorf("launchctl bootstrap: %v: %s", err, strings.TrimSpace(string(out)))
	}
	fmt.Fprintf(os.Stderr, "this Mac now sends digests (mode %s): %d scheduled times, hourly retries at :35\nlog: %s\n", modeName, len(times), logPath)
	return nil
}

// installedBinary prefers the installed cratecli over whatever is running,
// so the schedule survives rebuilding bin/cratecli.
func installedBinary() (string, error) {
	if p, err := exec.LookPath("cratecli"); err == nil {
		return filepath.Abs(p)
	}
	p, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(p)
}

func launchdDomain() string { return "gui/" + strconv.Itoa(os.Getuid()) }

func localOff(say bool) error {
	path, err := launchdPlistPath()
	if err != nil {
		return err
	}
	_ = exec.Command("launchctl", "bootout", launchdDomain()+"/"+launchdLabel).Run()
	err = os.Remove(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		if say {
			fmt.Fprintln(os.Stderr, "this Mac wasn't sending digests")
		}
		return nil
	case err != nil:
		return err
	}
	if say {
		fmt.Fprintln(os.Stderr, "this Mac no longer sends digests")
	}
	return nil
}

func (a *app) localStatus(cmd *cobra.Command) error {
	path, err := launchdPlistPath()
	if err != nil {
		return err
	}
	w := cmd.OutOrStdout()
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		fmt.Fprintln(w, "off: this Mac doesn't send digests (cratecli digest local on)")
		return nil
	}
	if err != nil {
		return err
	}
	mode := "on"
	if bytes.Contains(b, []byte("--fallback")) {
		mode = "auto"
	}
	loaded := exec.Command("launchctl", "print", launchdDomain()+"/"+launchdLabel).Run() == nil
	fmt.Fprintf(w, "%s: this Mac sends digests%s\n", mode, map[bool]string{true: " only when the server hasn't"}[mode == "auto"])
	fmt.Fprintf(w, "launchd: %s (%s)\n", map[bool]string{true: "loaded", false: "NOT loaded; run `cratecli digest local " + mode + "` again"}[loaded], path)
	if lib := a.library(); lib != nil {
		if runs, _ := lib.Runs(cmd.Context(), "", "", 3); len(runs) > 0 {
			fmt.Fprintln(w, "recent:")
			for _, r := range runs {
				fmt.Fprintf(w, "  %s  %s %s  %s\n", ago(r.FinishedAt), r.Digest, r.Status, r.Message)
			}
		}
	}
	return nil
}

func fallbackGrace(f *digest.File) time.Duration {
	g, err := time.ParseDuration(f.Fallback.Grace)
	if err != nil || f.Fallback.Grace == "" {
		return 15 * time.Minute
	}
	return g
}

// notSentByServer keeps the due digests the server hasn't sent today,
// once their time plus the grace period has passed. If the server can't
// be reached, everything due is kept: that's what a fallback is for.
func (a *app) notSentByServer(cmd *cobra.Command, f *digest.File, r *digest.Runner, due []digest.Digest) ([]digest.Digest, error) {
	if f.Fallback.Server == "" {
		return nil, fmt.Errorf("--fallback needs fallback.server in digests.yaml")
	}
	loc, err := f.Location()
	if err != nil {
		return nil, err
	}
	now := r.Now().In(loc)
	grace := fallbackGrace(f)
	var out []digest.Digest
	for _, d := range due {
		at, _ := d.ScheduledOn(now, loc)
		if now.Before(at.Add(grace)) {
			continue // give the server its chance first
		}
		token := f.Fallback.Token
		if token == "" {
			token = os.Getenv("CRATECLI_API_TOKEN")
		}
		sent, err := serverSent(cmd.Context(), f.Fallback.Server, token, d.Name, now.Format("2006-01-02"))
		switch {
		case err != nil:
			a.logf("server unreachable (%v); sending %s from this Mac", err, d.Name)
			out = append(out, d)
		case sent:
			a.logf("%s: the server already sent it", d.Name)
		default:
			a.logf("%s: the server hasn't sent it; sending from this Mac", d.Name)
			out = append(out, d)
		}
	}
	return out, nil
}

// serverSent asks a cratecli API whether it sent digest on date.
func serverSent(ctx context.Context, server, token, name, date string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	u := strings.TrimRight(server, "/") + "/v1/digests/" + url.PathEscape(name) + "/runs?date=" + url.QueryEscape(date)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return false, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("%s: HTTP %d", u, resp.StatusCode)
	}
	var body struct {
		Runs []struct {
			Status string `json:"status"`
		} `json:"runs"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return false, err
	}
	for _, r := range body.Runs {
		if r.Status == "sent" || r.Status == "skipped" {
			return true, nil
		}
	}
	return false, nil
}
