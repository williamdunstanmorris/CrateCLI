package core

import (
	"context"
	"fmt"
	"strings"

	"github.com/williamdunstanmorris/cratecli/internal/crate"
	"github.com/williamdunstanmorris/cratecli/internal/styles"
)

// DigRequest describes one dig. It is the common language of cratecli:
// `cratecli from …` and `cratecli search …` build one from flags, a digest
// section is one written in YAML, and the API receives one as JSON.
//
// Exactly one of From or Search is set.
type DigRequest struct {
	// From lists curated sources in CLI syntax: "hardwax", "oye:new",
	// "bbc:late-junction", "@berlin".
	From []string `json:"from,omitempty" yaml:"from,omitempty"`
	// Search is a Discogs database search.
	Search *SearchRequest `json:"search,omitempty" yaml:"search,omitempty"`

	// Filters for From (applied after the Discogs lookup).
	Style string   `json:"style,omitempty" yaml:"style,omitempty"`
	Year  string   `json:"year,omitempty" yaml:"year,omitempty"`
	Tags  []string `json:"tags,omitempty" yaml:"tags,omitempty"` // Bandcamp tags
	// Episodes is how many recent radio episodes to read (default 1).
	Episodes int `json:"episodes,omitempty" yaml:"episodes,omitempty"`

	Limit       Limit `json:"limit,omitzero" yaml:"limit,omitempty"`
	Random      bool  `json:"random,omitempty" yaml:"random,omitempty"`
	Fast        bool  `json:"fast,omitempty" yaml:"fast,omitempty"`
	IncludeSeen bool  `json:"include_seen,omitempty" yaml:"include_seen,omitempty"`
	// HideSeen hides seen records for every kind of dig. From and random
	// searches always do; digests set it so plain searches don't repeat
	// the same records every week.
	HideSeen bool `json:"hide_seen,omitempty" yaml:"-"`

	VideoSource string `json:"video_source,omitempty" yaml:"video_source,omitempty"`
	MaxVideos   int    `json:"max_videos,omitempty" yaml:"max_videos,omitempty"`
}

// DigResult is what a dig found.
type DigResult struct {
	Records []crate.Record `json:"records"`
	Stats   Stats          `json:"stats"`
	// Track says whether these records should be marked seen when the
	// dig is recorded (from and --random digs).
	Track bool `json:"-"`
}

// Validate checks a request without running it.
func (s *Service) Validate(req DigRequest) error {
	_, err := s.plan(req)
	return err
}

// Dig runs a request through the pipeline.
func (s *Service) Dig(ctx context.Context, req DigRequest) (*DigResult, error) {
	p, err := s.plan(req)
	if err != nil {
		return nil, err
	}
	recs, st, err := s.Run(ctx, p.fetch, p.opts)
	if err != nil {
		return nil, err
	}
	return &DigResult{Records: recs, Stats: st, Track: p.opts.Track}, nil
}

// digPlan is a validated request: what to fetch and how to process it.
type digPlan struct {
	fetch Fetcher
	opts  RunOptions
}

func (s *Service) plan(req DigRequest) (digPlan, error) {
	if (len(req.From) == 0) == (req.Search == nil) {
		return digPlan{}, fmt.Errorf("a dig needs exactly one of from or search")
	}
	src, err := crate.ParseVideoSource(orDefault(req.VideoSource, string(crate.VideosAuto)))
	if err != nil {
		return digPlan{}, err
	}
	opts := RunOptions{
		Limit:       req.Limit,
		IncludeSeen: req.IncludeSeen,
		Fast:        req.Fast,
		VideoSource: src,
		MaxVideos:   req.MaxVideos,
	}
	if opts.Limit.IsZero() {
		opts.Limit = DefaultLimit
	}

	if req.Search != nil {
		if req.Search.Empty() {
			return digPlan{}, fmt.Errorf("give some free text or at least one filter (see --help)")
		}
		params, err := req.Search.Params(s.logf)
		if err != nil {
			return digPlan{}, err
		}
		opts.Track = req.Random || req.HideSeen
		size := FetchSize(opts.Limit, opts.Track, opts.IncludeSeen)
		return digPlan{
			fetch: func(ctx context.Context) ([]crate.Record, error) {
				return crate.FromSearch(ctx, s.Discogs, params, size, req.Random)
			},
			opts: opts,
		}, nil
	}

	picks, err := s.ParsePicks(req.From)
	if err != nil {
		return digPlan{}, err
	}
	if req.Style != "" || req.Year != "" {
		var yr *styles.YearRange
		if req.Year != "" {
			r, err := styles.ParseYears(req.Year)
			if err != nil {
				return digPlan{}, err
			}
			yr = &r
		}
		style := req.Style
		opts.Keep = func(r crate.Record) bool { return MatchesStyle(r, style) && MatchesYear(r, yr) }
		if req.Fast {
			return digPlan{}, fmt.Errorf("--style/--year need the Discogs lookup; drop --fast")
		}
	}
	if req.Episodes < 0 {
		return digPlan{}, fmt.Errorf("--episodes must be at least 1")
	}
	opts.Track, opts.Match, opts.NeedDiscogs = true, true, opts.Keep != nil
	fo := FetchOptions{
		Want:     FetchSize(opts.Limit, true, opts.IncludeSeen),
		All:      opts.Limit.All,
		Tags:     req.Tags,
		Random:   req.Random,
		Episodes: max(req.Episodes, 1),
	}
	return digPlan{
		fetch: func(ctx context.Context) ([]crate.Record, error) { return s.FetchPicks(ctx, picks, fo) },
		opts:  opts,
	}, nil
}

// RadioOnly reports whether every source in from reads radio episodes;
// radio digs show the whole tracklist unless a limit is given.
func (s *Service) RadioOnly(from []string) bool {
	picks, err := s.ParsePicks(from)
	if err != nil || len(picks) == 0 {
		return false
	}
	for _, p := range picks {
		if !p.Source.Info().Episodes {
			return false
		}
	}
	return true
}

func orDefault(s, d string) string {
	if strings.TrimSpace(s) == "" {
		return d
	}
	return s
}
