package crate

import (
	"context"
	"crypto/sha1"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"

	"github.com/williamdunstanmorris/cratecli/internal/cache"
	"github.com/williamdunstanmorris/cratecli/internal/discogs"
)

// Matcher finds the Discogs release for records that came from shops, press
// or radio, which only know artist/title/label/catno.
type Matcher struct {
	Discogs *discogs.Client
	Cache   *cache.Cache // may be nil
}

type matchResult struct {
	ReleaseID int    `json:"id"`
	URI       string `json:"uri"`
	Found     bool   `json:"found"`
	At        time.Time
}

// Match sets ReleaseID and DiscogsURL on r when a confident match exists. It
// reports whether it found one. Costs up to two Discogs searches (cached).
func (m *Matcher) Match(ctx context.Context, r *Record) (bool, error) {
	if r.ReleaseID > 0 {
		return true, nil
	}
	if r.Title == "" {
		return false, nil
	}
	key := "match-" + cacheKey(r.Artist, r.Title, r.CatNo)
	var cached matchResult
	if m.Cache.Get(key, &cached) {
		// Retry misses after a day; hits are good for the cache's lifetime.
		if cached.Found || time.Since(cached.At) < 24*time.Hour {
			if cached.Found {
				r.ReleaseID, r.DiscogsURL = cached.ReleaseID, discogsWeb+cached.URI
			}
			return cached.Found, nil
		}
	}

	var queries []discogs.SearchParams
	if r.CatNo != "" {
		queries = append(queries, discogs.SearchParams{Type: "release", CatNo: r.CatNo, PerPage: 25})
	}
	queries = append(queries, discogs.SearchParams{
		Type: "release", Artist: CleanArtist(r.Artist), ReleaseTitle: stripEdition(r.Title), PerPage: 25,
	})
	// "A / B" credits often fail Discogs' artist filter; retry with the first name.
	if p := primaryArtist(r.Artist); p != CleanArtist(r.Artist) {
		queries = append(queries, discogs.SearchParams{
			Type: "release", Artist: p, ReleaseTitle: stripEdition(r.Title), PerPage: 25,
		})
	}
	if r.Artist == "" {
		queries[len(queries)-1] = discogs.SearchParams{Type: "release", Query: r.Title, PerPage: 25}
	}
	if r.TrackOnly {
		if r.Artist == "" {
			return false, nil
		}
		queries = []discogs.SearchParams{{Type: "release", Artist: primaryArtist(r.Artist), Track: r.Title, PerPage: 25}}
	}

	var res matchResult
	for _, q := range queries {
		resp, err := m.Discogs.Search(ctx, q)
		if err != nil {
			return false, err
		}
		if best, ok := pickBest(r, resp.Results); ok {
			res = matchResult{ReleaseID: best.ID, URI: best.URI, Found: true}
			break
		}
	}
	res.At = time.Now()
	m.Cache.Put(key, res)
	if res.Found {
		r.ReleaseID, r.DiscogsURL = res.ReleaseID, discogsWeb+res.URI
	}
	return res.Found, nil
}

// pickBest chooses the result whose artist and title best match r, requiring
// a reasonable similarity so we never attach the wrong record.
func pickBest(r *Record, results []discogs.SearchResult) (discogs.SearchResult, bool) {
	wantA, wantT := fold(CleanArtist(r.Artist)), fold(stripEdition(r.Title))
	wantLabel := fold(r.Label)
	bestScore := 0.0
	var best discogs.SearchResult
	for _, res := range results {
		a, t := SplitTitle(res.Title)
		a, t = fold(CleanArtist(a)), fold(stripEdition(t))
		var score float64
		switch {
		case r.TrackOnly:
			// Discogs already filtered by track; the release title won't match.
			score = similarity(wantA, a) * 3
		case wantA != "":
			score = similarity(wantT, t)*2 + similarity(wantA, a)
		default:
			score = similarity(wantT, t)*2 + 0.5
		}
		if wantLabel != "" {
			for _, l := range res.Label {
				if strings.Contains(fold(l), wantLabel) || strings.Contains(wantLabel, fold(l)) {
					score += 0.3
					break
				}
			}
		}
		// Prefer vinyl a touch, since these come from record shops.
		for _, f := range res.Format {
			if f == "Vinyl" {
				score += 0.05
				break
			}
		}
		if score > bestScore {
			bestScore, best = score, res
		}
	}
	// 3.0 is a perfect artist+title match; demand most of it.
	return best, bestScore >= 2.3
}

var (
	editionRe = regexp.MustCompile(`(?i)\s*[\(\[][^\)\]]*\b(edition|remaster(ed)?|reissue|repress(ed)?|anniversary|deluxe|ltd|limited|lp|ep|\d*x?12inch|vinyl|cd|incl)\b[^\)\]]*[\)\]]`)
	formatRe  = regexp.MustCompile(`(?i)\s*[\(\[]\s*(\d+\s*x\s*)?(7|10|12)("|''|inch)[^\)\]]*[\)\]]`)
)

// stripEdition removes "(30th Anniversary Edition)", "(LP)", "(2x12")" etc.
func stripEdition(s string) string {
	s = editionRe.ReplaceAllString(s, "")
	s = formatRe.ReplaceAllString(s, "")
	return strings.TrimSpace(s)
}

var nonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

var artistSep = regexp.MustCompile(`(?i)\s*(?:/|,|&|\band\b|\bx\b|\bvs\.?|\bfeat\.?|\bfeaturing\b|\bft\.?)\s+`)

// primaryArtist returns the first of several credited artists, since
// Discogs' artist filter fails on "A / B" style credits.
func primaryArtist(s string) string {
	s = CleanArtist(s)
	if parts := artistSep.Split(s, 2); len(parts) > 0 && strings.TrimSpace(parts[0]) != "" {
		return strings.TrimSpace(parts[0])
	}
	return s
}

// fold lowercases, strips accents and punctuation: "Pépé Bradock" -> "pepe bradock".
func fold(s string) string {
	t := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	out, _, err := transform.String(t, s)
	if err != nil {
		out = s
	}
	out = strings.ToLower(out)
	out = strings.ReplaceAll(out, "&", " and ")
	out = strings.TrimPrefix(out, "the ")
	return strings.TrimSpace(nonAlnum.ReplaceAllString(out, " "))
}

// similarity is a 0..1 score: 1 for equal, high when one contains the other,
// otherwise token overlap.
func similarity(a, b string) float64 {
	switch {
	case a == "" || b == "":
		return 0
	case a == b:
		return 1
	case strings.Contains(a, b) || strings.Contains(b, a):
		return 0.85
	}
	ta, tb := strings.Fields(a), strings.Fields(b)
	set := map[string]bool{}
	for _, w := range tb {
		set[w] = true
	}
	common := 0
	for _, w := range ta {
		if set[w] {
			common++
		}
	}
	return float64(2*common) / float64(len(ta)+len(tb))
}

func cacheKey(parts ...string) string {
	return fmt.Sprintf("%x", sha1.Sum([]byte(fold(strings.Join(parts, "|")))))
}
