// Package crate holds the source-agnostic Record model and the pipeline that
// pulls records from Discogs and attaches listenable links.
package crate

import (
	"crypto/sha1"
	"encoding/hex"
	"regexp"
	"strconv"
	"strings"
)

// Link is a playable URL for a record.
type Link struct {
	URL   string `json:"url"`
	Title string `json:"title,omitempty"`
	// Source is "discogs" (curated on the release page) or "youtube-api".
	Source string `json:"source"`
}

// Record is one item in a dig, regardless of where it came from.
type Record struct {
	ReleaseID  int      `json:"release_id"`
	Artist     string   `json:"artist"`
	Title      string   `json:"title"`
	Label      string   `json:"label,omitempty"`
	CatNo      string   `json:"catno,omitempty"`
	Format     string   `json:"format,omitempty"`
	Year       string   `json:"year,omitempty"`
	Country    string   `json:"country,omitempty"`
	Genres     []string `json:"genres,omitempty"`
	Styles     []string `json:"styles,omitempty"`
	Want       int      `json:"want,omitempty"`
	Have       int      `json:"have,omitempty"`
	DiscogsURL string   `json:"discogs_url"`

	// Source says where the record came from, e.g. "search", "hardwax:this-week".
	Source string `json:"source"`
	// Kind is the source's kind: database, shop, press, radio or mixed.
	Kind string `json:"kind,omitempty"`
	// Note is the source's own words about the record (a shop's blurb, a review link).
	Note string `json:"note,omitempty"`
	// SourceURL is the record's page at the source (shop listing, review…).
	SourceURL string `json:"source_url,omitempty"`
	// Tags are the source's own genre labels (e.g. OYE's "US House").
	Tags []string `json:"tags,omitempty"`

	// Marketplace fields, set only for seller listings.
	ListingURL string `json:"listing_url,omitempty"`
	Price      string `json:"price,omitempty"`
	Condition  string `json:"condition,omitempty"`
	Listed     string `json:"listed,omitempty"`

	Videos []Link `json:"videos"`
	// Audio holds direct audio links offered by the source (Bandcamp streams,
	// shop snippets). They may expire.
	Audio []Link `json:"audio,omitempty"`
	// ListenURL is a page where the record can be played in full, when the
	// source has one (e.g. the Bandcamp album page).
	ListenURL        string `json:"listen_url,omitempty"`
	YouTubeSearchURL string `json:"youtube_search_url"`

	// Saved fields, set on records listed by `cratecli saved`.
	SavedAt string `json:"saved_at,omitempty"`
	MyNote  string `json:"my_note,omitempty"`
	// Origin is how it was saved: "crate" (cratecli save) or "youtube" (a like).
	Origin string `json:"origin,omitempty"`

	// TrackOnly marks records that name a single track (radio tracklists)
	// rather than a release, so matching and video picking work per track.
	TrackOnly bool `json:"-"`
}

// Query is the "Artist - Title" string used for YouTube searches, without
// edition noise like "(30th Anniversary Edition)" or "(2x12")".
func (r Record) Query() string {
	a, t := CleanArtist(r.Artist), stripEdition(r.Title)
	if t == "" {
		t = r.Title
	}
	if a == "" || strings.EqualFold(a, "various") {
		return t
	}
	return a + " - " + t
}

// BestURL is the most useful single link for the record: the first video,
// then a full-listen page from the source, then the YouTube search page.
func (r Record) BestURL() string {
	switch {
	case len(r.Videos) > 0:
		return r.Videos[0].URL
	case r.ListenURL != "":
		return r.ListenURL
	}
	return r.YouTubeSearchURL
}

var (
	disambig = regexp.MustCompile(`\s*\(\d+\)`) // "Willmo (2)" -> "Willmo"
	anvStar  = regexp.MustCompile(`\*`)         // "Artist*" (name variation marker)
)

// CleanArtist strips Discogs-specific decoration from artist names.
func CleanArtist(s string) string {
	s = disambig.ReplaceAllString(s, "")
	s = anvStar.ReplaceAllString(s, "")
	return strings.TrimSpace(s)
}

// SplitTitle splits a search result title "Artist - Title" into its parts.
func SplitTitle(s string) (artist, title string) {
	if a, t, ok := strings.Cut(s, " - "); ok {
		return strings.TrimSpace(a), strings.TrimSpace(t)
	}
	return "", strings.TrimSpace(s)
}

// Key identifies a record across sources and runs, so seen history and
// saves recognise it whether it came from Hard Wax, OYE or a search:
// folded artist and title without edition noise, e.g. "basic channel|q 1 1".
// Records with no artist or title fall back to the Discogs release ID.
func (r Record) Key() string {
	a := fold(CleanArtist(r.Artist))
	if strings.HasPrefix(a, "various") {
		a = "various"
	}
	t := fold(stripEdition(r.Title))
	if t == "" {
		t = fold(r.Title)
	}
	if t == "" {
		if r.ReleaseID > 0 {
			return "release:" + strconv.Itoa(r.ReleaseID)
		}
		return ""
	}
	return a + "|" + t
}

// ID is a short, stable identifier for the record, safe in URLs:
// "rec_" and the first 12 hex digits of the SHA-1 of its Key.
func (r Record) ID() string {
	k := r.Key()
	if k == "" {
		return ""
	}
	sum := sha1.Sum([]byte(k))
	return "rec_" + hex.EncodeToString(sum[:6])
}
