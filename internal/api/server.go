// Package api is cratecli's HTTP API (`cratecli serve`).
//
// Handlers are thin: decode JSON into the same request types the CLI
// builds from flags (core.DigRequest…), call the service layer, encode
// the result. Routing uses the standard library's http.ServeMux patterns
// ("GET /v1/sources/{name}"), so there's no framework to learn.
//
// The contract is documented in api/openapi.yaml.
package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/williamdunstanmorris/cratecli/internal/core"
	"github.com/williamdunstanmorris/cratecli/internal/digest"
	"github.com/williamdunstanmorris/cratecli/internal/mail"
	"github.com/williamdunstanmorris/cratecli/internal/youtube"
)

// Server serves the API.
type Server struct {
	Service *core.Service
	// Digests loads digests.yaml (re-read per request, so edits apply
	// without a restart).
	Digests func() (*digest.File, error)
	// Sender is the mail account; nil means digests can't be sent.
	Sender mail.Sender
	// YouTube returns the signed-in account (for /v1/sync/youtube).
	YouTube func(ctx context.Context) (*youtube.Account, error)
	// Token, when set, is required as "Authorization: Bearer <token>" on
	// /v1 routes. Health and metrics stay open for probes and scrapers.
	Token   string
	Log     *slog.Logger
	Version string

	metrics metrics
}

// Handler returns the routes wrapped in logging, auth and metrics.
func (s *Server) Handler() http.Handler {
	if s.Log == nil {
		s.Log = slog.Default()
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.healthz)
	mux.HandleFunc("GET /readyz", s.readyz)
	mux.HandleFunc("GET /metrics", s.metrics.serve)

	v1 := func(pattern string, h http.HandlerFunc) { mux.Handle(pattern, s.auth(h)) }
	v1("GET /v1/sources", s.listSources)
	v1("GET /v1/sources/{name}", s.getSource)
	v1("POST /v1/digs", s.createDig)
	v1("GET /v1/digs", s.listDigs)
	v1("GET /v1/digs/{id}", s.getDig)
	v1("GET /v1/saved", s.listSaved)
	v1("POST /v1/saved", s.save)
	v1("DELETE /v1/saved/{id}", s.unsave)
	v1("GET /v1/seen", s.seenStats)
	v1("POST /v1/seen/undo", s.undoSeen)
	v1("POST /v1/sync/youtube", s.syncYouTube)
	v1("GET /v1/digests", s.listDigests)
	v1("GET /v1/digests/{name}/preview", s.previewDigest)
	v1("GET /v1/digests/{name}/runs", s.digestRuns)
	v1("GET /v1/digest-runs", s.listRuns)
	v1("POST /v1/digest-runs", s.createRun)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		problem(w, http.StatusNotFound, "no such endpoint", r.Method+" "+r.URL.Path)
	})
	return s.observe(mux)
}

func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.Token != "" {
			got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
			if !ok || subtle.ConstantTimeCompare([]byte(got), []byte(s.Token)) != 1 {
				w.Header().Set("WWW-Authenticate", "Bearer")
				problem(w, http.StatusUnauthorized, "missing or wrong API token", "send Authorization: Bearer <token>")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// observe logs each request as JSON and counts it for /metrics.
func (s *Server) observe(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		route := r.Pattern
		if route == "" {
			route = "unmatched"
		}
		s.metrics.request(route, rec.status, time.Since(start))
		if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" || r.URL.Path == "/metrics" {
			return // probes would drown the log
		}
		s.Log.Info("request", "method", r.Method, "path", r.URL.Path, "route", route,
			"status", rec.status, "duration_ms", time.Since(start).Milliseconds())
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (s *Server) healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": s.Version})
}

// readyz checks the database: no point routing traffic here without it.
func (s *Server) readyz(w http.ResponseWriter, r *http.Request) {
	if lib := s.Service.Library; lib != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := lib.Ping(ctx); err != nil {
			problem(w, http.StatusServiceUnavailable, "database unavailable", err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

// ---------------------------------------------------------------- helpers

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

// problem writes an RFC 9457 error: {"title", "status", "detail"}.
func problem(w http.ResponseWriter, status int, title, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"type": "about:blank", "title": title, "status": status, "detail": detail,
	})
}

// decode reads a JSON body into v, rejecting unknown fields so typos in
// requests are errors rather than silently ignored.
func decode(r *http.Request, v any) error {
	if r.Body == nil || r.ContentLength == 0 {
		return nil
	}
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("invalid JSON body: %w", err)
	}
	return nil
}

// badRequest is for errors caused by the request (validation).
var errBadRequest = errors.New("bad request")

func fail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errBadRequest):
		problem(w, http.StatusBadRequest, "bad request", strings.TrimPrefix(err.Error(), "bad request: "))
	case errors.Is(err, context.DeadlineExceeded):
		problem(w, http.StatusGatewayTimeout, "took too long", err.Error())
	default:
		problem(w, http.StatusInternalServerError, "something went wrong", err.Error())
	}
}

func bad(err error) error { return fmt.Errorf("%w: %w", errBadRequest, err) }
