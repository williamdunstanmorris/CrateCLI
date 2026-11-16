package cli

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/williamdunstanmorris/cratecli/internal/api"
	"github.com/williamdunstanmorris/cratecli/internal/core"
	"github.com/williamdunstanmorris/cratecli/internal/digest"
	"github.com/williamdunstanmorris/cratecli/internal/mail"
	"github.com/williamdunstanmorris/cratecli/internal/youtube"
)

func (a *app) serveCmd() *cobra.Command {
	var addr string
	var schedule bool
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the HTTP API (and optionally the digest scheduler)",
		Long: `Serves cratecli over HTTP: the same digs, saves and digests as the CLI,
as JSON. The contract is in api/openapi.yaml.

Configuration comes from the same places as the CLI: config.json in
CRATECLI_CONFIG_DIR (or the usual config folder), overridden by
environment variables, which suit containers best:

  DISCOGS_TOKEN                      Discogs personal access token
  CRATECLI_CONFIG_DIR                folder with config.json, digests.yaml, YouTube token
  CRATECLI_LIBRARY                   database file (default <config dir>/library.db)
  CRATECLI_DIGESTS                   digests file (default <config dir>/digests.yaml)
  CRATECLI_SMTP_HOST/_PORT/_USERNAME/_PASSWORD   mail account
  CRATECLI_YOUTUBE_CLIENT_ID/_SECRET             YouTube OAuth client
  CRATECLI_API_TOKEN                 require "Authorization: Bearer <token>" on /v1
  CRATECLI_ADDR                      listen address (default :8080)

Logs are JSON on stderr. It shuts down cleanly on SIGTERM, finishing
requests in flight (up to 30s).

--schedule also sends due digests itself, checking every minute. Leave it
off when something else triggers them (POST /v1/digest-runs {"due": true}).`,
		Example: `  cratecli serve
  cratecli serve --addr 127.0.0.1:8080 --schedule
  curl -s localhost:8080/v1/digs -d '{"from":["hardwax"],"fast":true,"limit":5}'`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if addr == "" {
				addr = os.Getenv("CRATECLI_ADDR")
			}
			if addr == "" {
				addr = ":8080"
			}
			log := slog.New(slog.NewJSONHandler(os.Stderr, nil))
			// Progress messages from digs become debug-level noise here.
			a.flags.quiet = true

			svc := a.core()
			if svc.Library == nil {
				return errors.New("serve needs the library database (see CRATECLI_LIBRARY)")
			}
			var sender mail.Sender
			if s, err := a.sender(); err == nil {
				sender = s
			} else {
				log.Warn("mail not configured; digests can't be sent", "error", err.Error())
			}
			loadDigests := func() (*digest.File, error) {
				path, err := digest.DefaultPath()
				if err != nil {
					return nil, err
				}
				f, err := digest.Load(path)
				if err != nil {
					return nil, err
				}
				return f, f.Validate(svc)
			}
			srv := &api.Server{
				Service: svc,
				Digests: loadDigests,
				Sender:  sender,
				YouTube: func(ctx context.Context) (*youtube.Account, error) { return a.youtubeAccount(cmd) },
				Token:   os.Getenv("CRATECLI_API_TOKEN"),
				Log:     log,
				Version: Version,
			}
			if srv.Token == "" {
				log.Warn("CRATECLI_API_TOKEN not set: /v1 is open to anyone who can reach " + addr)
			}

			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			hs := &http.Server{
				Addr:              addr,
				Handler:           srv.Handler(),
				ReadHeaderTimeout: 10 * time.Second,
				// Digs that look records up on Discogs can take minutes.
				WriteTimeout: 15 * time.Minute,
				IdleTimeout:  2 * time.Minute,
			}
			if schedule {
				if sender == nil {
					return errors.New("--schedule needs mail configured")
				}
				go scheduler(ctx, log, svc, loadDigests, sender)
			}
			errc := make(chan error, 1)
			go func() { errc <- hs.ListenAndServe() }()
			log.Info("listening", "addr", addr, "version", Version, "schedule", schedule)

			select {
			case err := <-errc:
				return err
			case <-ctx.Done():
			}
			log.Info("shutting down")
			shutdown, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			return hs.Shutdown(shutdown)
		},
	}
	cmd.Flags().StringVar(&addr, "addr", "", "listen address (default $CRATECLI_ADDR or :8080)")
	cmd.Flags().BoolVar(&schedule, "schedule", false, "send due digests from this process, checking every minute")
	return cmd
}

// scheduler sends due digests once a minute until ctx ends. Sending is
// idempotent per digest per day, so restarts and overlaps are harmless.
func scheduler(ctx context.Context, log *slog.Logger, svc *core.Service, load func() (*digest.File, error), sender mail.Sender) {
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		f, err := load()
		if err != nil {
			log.Error("scheduler: digests.yaml", "error", err.Error())
			continue
		}
		r := digest.NewRunner(svc, f, sender)
		due, err := r.Due(ctx)
		if err != nil {
			log.Error("scheduler: checking due digests", "error", err.Error())
			continue
		}
		for _, d := range due {
			res, err := r.Send(ctx, d, digest.SendOptions{})
			if err != nil {
				log.Error("scheduler: digest failed", "digest", d.Name, "error", err.Error())
				continue
			}
			log.Info("scheduler: digest", "digest", d.Name, "status", res.Run.Status,
				"records", res.Run.Records, "recipients", res.Run.Recipients)
		}
	}
}
