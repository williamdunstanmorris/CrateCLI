// Package core is cratecli's service layer: the operations the CLI, the
// email digests and the HTTP API all share.
//
// A request comes in as a plain struct (DigRequest), core does the work
// (fetch from sources, hide seen records, look up Discogs and videos) and
// returns plain data. core never prints tables or parses flags; the
// callers do that. That's what lets the same dig run from a terminal, a
// scheduled email or an HTTP request.
package core

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/williamdunstanmorris/cratecli/internal/cache"
	"github.com/williamdunstanmorris/cratecli/internal/config"
	"github.com/williamdunstanmorris/cratecli/internal/crate"
	"github.com/williamdunstanmorris/cratecli/internal/discogs"
	"github.com/williamdunstanmorris/cratecli/internal/library"
	"github.com/williamdunstanmorris/cratecli/internal/sources"
	"github.com/williamdunstanmorris/cratecli/internal/youtube"
)

// Service holds what every operation needs. Build one with New.
type Service struct {
	Config  config.Config
	Discogs *discogs.Client
	Sources map[string]sources.Source
	// Library is the local history (seen, digs, saved). Nil means none:
	// digs still work, without hiding seen records.
	Library *library.Library
	// Log receives progress and warnings, one message per call. Nil = silent.
	Log func(format string, args ...any)
	// NoCache bypasses the on-disk release and page caches.
	NoCache bool
}

// Options configures New.
type Options struct {
	Library *library.Library
	Log     func(format string, args ...any)
	NoCache bool
}

// New builds a Service from the user's config.
func New(cfg config.Config, o Options) *Service {
	s := &Service{
		Config:  cfg,
		Discogs: discogs.New(cfg.DiscogsToken, cfg.UserAgent),
		Library: o.Library,
		Log:     o.Log,
		NoCache: o.NoCache,
	}
	s.Sources = sources.Registry(sources.Env{
		HTTP:    sources.NewFetcher(cfg.UserAgent, o.NoCache),
		Discogs: s.Discogs,
		Log:     s.logf,
	})
	return s
}

func (s *Service) logf(format string, args ...any) {
	if s.Log != nil {
		s.Log(format, args...)
	}
}

// logWriter adapts Log to the io.Writer the enricher logs to.
type logWriter struct{ s *Service }

func (w logWriter) Write(p []byte) (int, error) {
	for _, line := range strings.Split(strings.TrimRight(string(p), "\n"), "\n") {
		w.s.logf("%s", line)
	}
	return len(p), nil
}

// EnrichOptions chooses how records are looked up.
type EnrichOptions struct {
	VideoSource crate.VideoSource // "" = auto
	MaxVideos   int               // 0 = 3
	Match       bool              // match records without a release ID on Discogs
	NeedDiscogs bool              // fetch release data even if videos come from elsewhere
}

// Enricher builds the Discogs/YouTube lookup pipeline.
func (s *Service) Enricher(o EnrichOptions) (*crate.Enricher, error) {
	if o.VideoSource == "" {
		o.VideoSource = crate.VideosAuto
	}
	if o.MaxVideos == 0 {
		o.MaxVideos = 3
	}
	var c *cache.Cache
	if !s.NoCache {
		c = cache.New("releases", 30*24*time.Hour)
	}
	yt := youtube.New(s.Config.YouTubeAPIKey)
	if o.VideoSource == crate.VideosYouTube && !yt.Enabled() {
		return nil, fmt.Errorf("--video-source youtube needs YOUTUBE_API_KEY")
	}
	e := &crate.Enricher{
		Discogs:     s.Discogs,
		YouTube:     yt,
		Cache:       c,
		Source:      o.VideoSource,
		MaxVideos:   o.MaxVideos,
		NeedDiscogs: o.NeedDiscogs,
	}
	if s.Log != nil {
		e.Log = logWriter{s}
	}
	if o.Match {
		e.Matcher = &crate.Matcher{Discogs: s.Discogs, Cache: c}
	}
	return e, nil
}

// stderrLog is a ready-made Log that writes to stderr.
func stderrLog(w io.Writer) func(string, ...any) {
	return func(format string, args ...any) { fmt.Fprintf(w, format+"\n", args...) }
}

// StderrLog logs to os.Stderr.
var StderrLog = stderrLog(os.Stderr)
