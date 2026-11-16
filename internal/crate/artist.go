package crate

import (
	"context"
	"fmt"
	"math/rand/v2"
	"strconv"
	"strings"

	"github.com/williamdunstanmorris/cratecli/internal/cache"
	"github.com/williamdunstanmorris/cratecli/internal/discogs"
)

// ArtistCandidate is a possible match for an artist name.
type ArtistCandidate struct {
	ID   int
	Name string
}

// AmbiguousArtistError lists several artists sharing a name.
type AmbiguousArtistError struct {
	Query      string
	Candidates []ArtistCandidate
}

func (e *AmbiguousArtistError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "several artists match %q; pick one with --id:\n", e.Query)
	for _, c := range e.Candidates {
		fmt.Fprintf(&b, "  --id %-9d %s\n", c.ID, c.Name)
	}
	return strings.TrimRight(b.String(), "\n")
}

// FindArtist resolves a name to a Discogs artist. An exact (case- and
// accent-insensitive) name match wins; Discogs disambiguates namesakes as
// "Name (2)", so several exact matches are reported as ambiguous.
func FindArtist(ctx context.Context, c *discogs.Client, name string) (ArtistCandidate, error) {
	resp, err := c.Search(ctx, discogs.SearchParams{Type: "artist", Query: name, PerPage: 25})
	if err != nil {
		return ArtistCandidate{}, err
	}
	want := fold(name)
	var exact, all []ArtistCandidate
	for _, r := range resp.Results {
		cand := ArtistCandidate{ID: r.ID, Name: r.Title}
		all = append(all, cand)
		if fold(CleanArtist(r.Title)) == want {
			exact = append(exact, cand)
		}
	}
	switch {
	case len(exact) == 1:
		return exact[0], nil
	case len(exact) > 1:
		return ArtistCandidate{}, &AmbiguousArtistError{Query: name, Candidates: exact}
	case len(all) == 0:
		return ArtistCandidate{}, fmt.Errorf("no artist called %q on Discogs", name)
	default:
		return ArtistCandidate{}, &AmbiguousArtistError{Query: name, Candidates: all[:min(len(all), 8)]}
	}
}

// ArtistOptions controls FromArtist.
type ArtistOptions struct {
	Appearances bool // include remixes, features and compilation appearances
	YearFrom    int  // 0 = no limit
	YearTo      int
	Limit       int  // number of tunes
	Newest      bool // take newest releases first instead of shuffling
	Cache       *cache.Cache
	Log         func(string, ...any)
}

// FromArtist returns individual tunes by an artist: it walks their releases
// (one entry per album, not per pressing), pools the videos linked on
// Discogs, and samples from that pool. Each record is one tune with one video.
func FromArtist(ctx context.Context, c *discogs.Client, artist ArtistCandidate, o ArtistOptions) ([]Record, error) {
	var entries []discogs.ArtistRelease
	skippedAppearances := 0
	for page := 1; page <= 5; page++ {
		resp, err := c.ArtistReleases(ctx, artist.ID, page)
		if err != nil {
			return nil, err
		}
		for _, r := range resp.Releases {
			if r.Role != "Main" && !o.Appearances {
				skippedAppearances++
				continue
			}
			if r.Role == "UnofficialRelease" {
				continue
			}
			if o.YearFrom > 0 && (r.Year < o.YearFrom || r.Year > o.YearTo) {
				continue
			}
			entries = append(entries, r)
		}
		if page >= resp.Pagination.Pages {
			break
		}
	}
	if len(entries) == 0 {
		if skippedAppearances > 0 && o.YearFrom == 0 {
			return nil, fmt.Errorf("%s has no releases of their own, but appears on %d others; add --appearances", artist.Name, skippedAppearances)
		}
		return nil, fmt.Errorf("no releases found for %s with those filters", artist.Name)
	}

	if o.Newest {
		for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
			entries[i], entries[j] = entries[j], entries[i]
		}
	} else {
		rand.Shuffle(len(entries), func(i, j int) { entries[i], entries[j] = entries[j], entries[i] })
	}

	// Take at most two tunes per release so a dig spans their catalogue,
	// and stop fetching once we have enough.
	perRelease := 2
	if len(entries)*2 < o.Limit {
		perRelease = o.Limit
	}
	seen := map[string]bool{}
	var tunes []Record
	looked := 0
	for _, e := range entries {
		if len(tunes) >= o.Limit || looked >= max(o.Limit*3, 12) {
			break
		}
		looked++
		videos, rel, err := releaseVideos(ctx, c, o.Cache, e)
		if err != nil {
			if o.Log != nil {
				o.Log("warn: %s: %v", e.Title, err)
			}
			continue
		}
		if !o.Newest {
			rand.Shuffle(len(videos), func(i, j int) { videos[i], videos[j] = videos[j], videos[i] })
		}
		taken := 0
		for _, v := range videos {
			if taken >= perRelease || len(tunes) >= o.Limit {
				break
			}
			if seen[v.URI] {
				continue
			}
			seen[v.URI] = true
			taken++
			tunes = append(tunes, tuneRecord(artist, e, rel, v))
		}
	}
	if len(tunes) == 0 {
		return nil, fmt.Errorf("found %d releases by %s but none have videos linked on Discogs", len(entries), artist.Name)
	}
	if !o.Newest {
		rand.Shuffle(len(tunes), func(i, j int) { tunes[i], tunes[j] = tunes[j], tunes[i] })
	}
	return tunes, nil
}

// releaseVideos fetches videos for a master (pooled across all its
// versions) or a single release. Cached like other release data.
func releaseVideos(ctx context.Context, c *discogs.Client, ch *cache.Cache, e discogs.ArtistRelease) ([]discogs.Video, *discogs.Release, error) {
	if e.Type == "master" {
		key := "master-" + strconv.Itoa(e.ID)
		var m discogs.Master
		if !ch.Get(key, &m) {
			got, err := c.Master(ctx, e.ID)
			if err != nil {
				return nil, nil, err
			}
			m = *got
			ch.Put(key, m)
		}
		return m.Videos, nil, nil
	}
	key := "release-" + strconv.Itoa(e.ID)
	var r discogs.Release
	if !ch.Get(key, &r) {
		got, err := c.Release(ctx, e.ID)
		if err != nil {
			return nil, nil, err
		}
		r = *got
		ch.Put(key, r)
	}
	return r.Videos, &r, nil
}

func tuneRecord(artist ArtistCandidate, e discogs.ArtistRelease, rel *discogs.Release, v discogs.Video) Record {
	title := v.Title
	// Video titles are usually "Artist - Track"; keep just the track.
	if a, t, ok := strings.Cut(title, " - "); ok {
		fa := fold(a)
		if strings.Contains(fa, fold(CleanArtist(artist.Name))) || (e.Artist != "" && strings.Contains(fa, fold(CleanArtist(e.Artist)))) {
			title = strings.TrimSpace(t)
		}
	}
	id := e.ID
	discogsURL := fmt.Sprintf("%s/release/%d", discogsWeb, e.ID)
	if e.Type == "master" {
		id = e.MainRelease
		discogsURL = fmt.Sprintf("%s/master/%d", discogsWeb, e.ID)
	}
	r := Record{
		ReleaseID:  id,
		Artist:     CleanArtist(e.Artist),
		Title:      title,
		Label:      e.Label,
		Format:     e.Format,
		DiscogsURL: discogsURL,
		Source:     "artist:" + CleanArtist(artist.Name),
		Kind:       "database",
		Note:       "from " + e.Title,
		Videos:     []Link{{URL: v.URI, Title: v.Title, Source: "discogs"}},
		TrackOnly:  true,
	}
	if r.Artist == "" {
		r.Artist = CleanArtist(artist.Name)
	}
	if e.Year > 0 {
		r.Year = strconv.Itoa(e.Year)
	}
	if e.Role != "Main" {
		r.Note += " (" + strings.ToLower(e.Role) + ")"
	}
	if rel != nil {
		r.Styles, r.Genres, r.Country = rel.Styles, rel.Genres, rel.Country
	}
	return r
}
