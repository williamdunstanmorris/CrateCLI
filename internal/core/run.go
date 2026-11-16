package core

import (
	"context"

	"github.com/williamdunstanmorris/cratecli/internal/crate"
)

// Fetcher produces candidate records: a page of search results, a shop's
// arrivals, a radio tracklist…
type Fetcher func(ctx context.Context) ([]crate.Record, error)

// RunOptions tweak the shared dig pipeline.
type RunOptions struct {
	Limit Limit
	// Track hides records seen before (unless IncludeSeen). The caller
	// marks the returned ones as seen when it records the dig.
	Track       bool
	IncludeSeen bool
	// Fast skips Discogs entirely: no matching, no videos, search links only.
	Fast bool
	// Match finds Discogs releases for records without an ID (shop picks).
	Match bool
	// Keep filters records after the Discogs lookup (style/year filters).
	Keep func(crate.Record) bool
	// NeedDiscogs forces release lookups (Keep needs styles and years).
	NeedDiscogs bool
	VideoSource crate.VideoSource
	MaxVideos   int
}

// Stats describe what a run did, for logs and API responses.
type Stats struct {
	Fetched    int `json:"fetched"`     // candidates the sources returned
	HiddenSeen int `json:"hidden_seen"` // dropped because already seen
	Looked     int `json:"looked"`      // candidates looked up on Discogs
	Returned   int `json:"returned"`
}

// Run is the pipeline every dig goes through:
//
//	fetch → hide seen → take N (or filter until N match) → Discogs + videos
//
// It returns the records ready to show. It doesn't record the dig; callers
// do that with Record once they've shown or sent the records.
func (s *Service) Run(ctx context.Context, fetch Fetcher, o RunOptions) ([]crate.Record, Stats, error) {
	var st Stats
	if o.Limit.IsZero() {
		o.Limit = DefaultLimit
	}
	if o.Fast {
		o.Match, o.NeedDiscogs = false, false
		o.VideoSource = crate.VideosNone
	}
	if !s.Discogs.Authenticated() && !o.Fast {
		s.logf("note: no DISCOGS_TOKEN set; using the slower anonymous rate limit (25 req/min)")
	}

	recs, err := fetch(ctx)
	if err != nil {
		return nil, st, err
	}
	st.Fetched = len(recs)
	if len(recs) == 0 {
		s.logf("no records found")
		return nil, st, nil
	}
	if o.Track && !o.IncludeSeen && s.Library != nil {
		total := len(recs)
		unseen, hidden, err := s.Library.Unseen(ctx, recs)
		if err != nil {
			s.logf("warn: seen history: %v", err)
		} else {
			recs, st.HiddenSeen = unseen, hidden
			if hidden > 0 {
				s.logf("hid %d of %d you've already seen (--seen shows them)", hidden, total)
			}
			if len(recs) == 0 {
				s.logf("nothing new: you've seen all %d. Try another section or source, or --seen", total)
				return nil, st, nil
			}
		}
	}

	e, err := s.Enricher(EnrichOptions{VideoSource: o.VideoSource, MaxVideos: o.MaxVideos, Match: o.Match, NeedDiscogs: o.NeedDiscogs})
	if err != nil {
		return nil, st, err
	}

	n := o.Limit.Of(len(recs))
	if o.Match && n > 40 {
		s.logf("tip: matching %d records on Discogs takes a few minutes the first time; --fast skips it", n)
	}
	if o.Keep == nil {
		recs = recs[:n]
		if o.Fast {
			s.logf("found %d records", len(recs))
		} else {
			s.logf("found %d records, looking up videos…", len(recs))
		}
		e.Enrich(ctx, recs)
		st.Looked = len(recs)
	} else {
		maxLook := min(len(recs), max(n*6, 30))
		if o.Limit.All {
			maxLook = len(recs)
		}
		s.logf("filtering up to %d candidates for %d matches…", maxLook, n)
		recs, st.Looked = e.EnrichUntil(ctx, recs, n, maxLook, o.Keep)
		if len(recs) < n {
			s.logf("only %d of %d candidates matched the filters", len(recs), st.Looked)
		}
	}
	st.Returned = len(recs)
	return recs, st, nil
}

// Record logs a dig in the library (numbered, so `save 3` works) and, with
// markSeen, adds its records to the seen history. Without a library it
// does nothing.
func (s *Service) Record(ctx context.Context, label string, recs []crate.Record, markSeen bool) (int64, error) {
	if s.Library == nil || len(recs) == 0 {
		return 0, nil
	}
	return s.Library.RecordDig(ctx, label, recs, markSeen)
}
