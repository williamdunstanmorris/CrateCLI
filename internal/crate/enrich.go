package crate

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/williamdunstanmorris/cratecli/internal/cache"
	"github.com/williamdunstanmorris/cratecli/internal/discogs"
	"github.com/williamdunstanmorris/cratecli/internal/youtube"
)

// VideoSource selects how videos are found.
type VideoSource string

const (
	// VideosAuto uses Discogs' curated videos, then falls back to the
	// YouTube API (if a key is set) for releases with none.
	VideosAuto VideoSource = "auto"
	// VideosDiscogs uses only videos linked on the Discogs release page.
	VideosDiscogs VideoSource = "discogs"
	// VideosYouTube always uses the YouTube Data API (costs quota).
	VideosYouTube VideoSource = "youtube"
	// VideosNone skips lookups; only the YouTube search URL is provided.
	VideosNone VideoSource = "none"
)

func ParseVideoSource(s string) (VideoSource, error) {
	switch v := VideoSource(strings.ToLower(s)); v {
	case VideosAuto, VideosDiscogs, VideosYouTube, VideosNone:
		return v, nil
	}
	return "", fmt.Errorf("unknown video source %q (want auto, discogs, youtube or none)", s)
}

// Enricher fetches full release data and attaches video links.
type Enricher struct {
	Discogs   *discogs.Client
	YouTube   *youtube.Client
	Cache     *cache.Cache // may be nil
	Source    VideoSource
	MaxVideos int
	Log       io.Writer // progress/warnings; never stdout
	// Matcher, when set, finds Discogs releases for records without an ID
	// (shop, press and radio picks).
	Matcher *Matcher
	// NeedDiscogs forces release lookups even when videos come from
	// elsewhere (e.g. to filter by style/year).
	NeedDiscogs bool

	quotaOnce sync.Once
	quotaHit  bool
	mu        sync.Mutex
}

// Enrich fills in videos and release details for every record, in place.
// Failures on individual records are logged, not fatal.
func (e *Enricher) Enrich(ctx context.Context, recs []Record) {
	const workers = 4
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	for i := range recs {
		wg.Add(1)
		sem <- struct{}{}
		go func(r *Record) {
			defer wg.Done()
			defer func() { <-sem }()
			e.enrichOne(ctx, r)
		}(&recs[i])
	}
	wg.Wait()
}

func (e *Enricher) logf(format string, args ...any) {
	if e.Log != nil {
		e.mu.Lock()
		fmt.Fprintf(e.Log, format+"\n", args...)
		e.mu.Unlock()
	}
}

func (e *Enricher) release(ctx context.Context, id int) (*discogs.Release, error) {
	key := "release-" + strconv.Itoa(id)
	var rel discogs.Release
	if e.Cache.Get(key, &rel) {
		return &rel, nil
	}
	r, err := e.Discogs.Release(ctx, id)
	if err != nil {
		return nil, err
	}
	e.Cache.Put(key, r)
	return r, nil
}

func (e *Enricher) enrichOne(ctx context.Context, r *Record) {
	needRelease := e.Source == VideosAuto || e.Source == VideosDiscogs || r.Title == "" || e.NeedDiscogs
	if needRelease && r.ReleaseID == 0 && e.Matcher != nil {
		if _, err := e.Matcher.Match(ctx, r); err != nil {
			e.logf("warn: matching %q on Discogs: %v", r.Query(), err)
		}
	}
	if needRelease && r.ReleaseID > 0 {
		rel, err := e.release(ctx, r.ReleaseID)
		if err != nil {
			e.logf("warn: release %d: %v", r.ReleaseID, err)
		} else {
			mergeRelease(r, rel)
			if e.Source != VideosYouTube && e.Source != VideosNone {
				want := fold(stripEdition(r.Title))
				for _, v := range rel.Videos {
					if len(r.Videos) >= e.MaxVideos {
						break
					}
					// For a single track, only keep videos of that track.
					if r.TrackOnly && !strings.Contains(fold(v.Title), want) {
						continue
					}
					r.Videos = append(r.Videos, Link{URL: v.URI, Title: v.Title, Source: "discogs"})
				}
			}
		}
	}

	r.YouTubeSearchURL = youtube.SearchURL(r.Query())

	wantAPI := e.Source == VideosYouTube || (e.Source == VideosAuto && len(r.Videos) == 0)
	if wantAPI && e.YouTube.Enabled() && !e.quotaExhausted() {
		ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		vids, err := e.YouTube.Search(ctx, r.Query(), e.MaxVideos)
		switch {
		case errors.Is(err, youtube.ErrQuotaExceeded):
			e.markQuota()
		case err != nil:
			e.logf("warn: youtube search %q: %v", r.Query(), err)
		}
		for _, v := range vids {
			r.Videos = append(r.Videos, Link{URL: v.URL, Title: v.Title, Source: "youtube-api"})
		}
	}
	if r.Videos == nil {
		r.Videos = []Link{}
	}
}

func (e *Enricher) quotaExhausted() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.quotaHit
}

func (e *Enricher) markQuota() {
	e.quotaOnce.Do(func() {
		e.mu.Lock()
		e.quotaHit = true
		e.mu.Unlock()
		e.logf("warn: YouTube API quota used up for today; falling back to search URLs")
	})
}

// mergeRelease fills gaps in r from the full release.
func mergeRelease(r *Record, rel *discogs.Release) {
	if r.Artist == "" && len(rel.Artists) > 0 {
		var b strings.Builder
		for i, a := range rel.Artists {
			b.WriteString(a.Name)
			if i < len(rel.Artists)-1 {
				j := strings.TrimSpace(a.Join)
				if j == "" || j == "," {
					b.WriteString(", ")
				} else {
					b.WriteString(" " + j + " ")
				}
			}
		}
		r.Artist = b.String()
	}
	if r.Title == "" {
		r.Title = rel.Title
	}
	if r.Label == "" && len(rel.Labels) > 0 {
		r.Label, r.CatNo = rel.Labels[0].Name, rel.Labels[0].CatNo
	}
	if r.Year == "" && rel.Year > 0 {
		r.Year = strconv.Itoa(rel.Year)
	}
	if r.Country == "" {
		r.Country = rel.Country
	}
	if len(r.Genres) == 0 {
		r.Genres = rel.Genres
	}
	if len(r.Styles) == 0 {
		r.Styles = rel.Styles
	}
	if r.Format == "" && len(rel.Formats) > 0 {
		f := rel.Formats[0]
		r.Format = strings.Join(append([]string{f.Name}, f.Descriptions...), ", ")
	}
	if r.Want == 0 && r.Have == 0 {
		r.Want, r.Have = rel.Community.Want, rel.Community.Have
	}
}

// EnrichUntil enriches candidates in batches, keeping those that pass keep,
// until it has n records, runs out, or has looked at maxLook candidates.
// It returns the kept records and how many candidates it examined.
func (e *Enricher) EnrichUntil(ctx context.Context, candidates []Record, n, maxLook int, keep func(Record) bool) ([]Record, int) {
	var out []Record
	looked := 0
	for looked < len(candidates) && len(out) < n && looked < maxLook {
		batch := n - len(out)
		if keep != nil {
			batch = max(batch*2, 4) // filtering discards some; overfetch a little
		}
		end := min(looked+batch, len(candidates), maxLook)
		chunk := candidates[looked:end]
		e.Enrich(ctx, chunk)
		looked = end
		for _, r := range chunk {
			if keep == nil || keep(r) {
				out = append(out, r)
				if len(out) == n {
					break
				}
			}
		}
		if err := ctx.Err(); err != nil {
			break
		}
	}
	return out, looked
}
