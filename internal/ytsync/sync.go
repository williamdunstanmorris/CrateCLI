// Package ytsync keeps your saved records in step with your YouTube likes:
// every video you like on YouTube becomes a saved record in cratecli.
//
// Every like is kept, music or not. Titles are split into artist and
// title where they can be ("Artist - Title (Official Video)", or a
// "Artist - Topic" channel); otherwise the whole title is the title.
package ytsync

import (
	"context"
	"regexp"
	"strings"

	"github.com/williamdunstanmorris/cratecli/internal/crate"
	"github.com/williamdunstanmorris/cratecli/internal/library"
	"github.com/williamdunstanmorris/cratecli/internal/youtube"
)

// Likes is the part of youtube.Account the sync needs (an interface, so
// tests can use a fake).
type Likes interface {
	Likes(ctx context.Context, pageToken string) (youtube.LikesPage, error)
}

// Options control a sync.
type Options struct {
	// Full reads every like instead of stopping at the first page of
	// already-synced ones, and notices unlikes. The first sync is always full.
	Full   bool
	DryRun bool // report what would happen, change nothing
	Log    func(string, ...any)
}

// Result reports a sync.
type Result struct {
	Pages     int      `json:"pages"`      // API calls (1 quota unit each)
	Checked   int      `json:"checked"`    // likes looked at
	New       int      `json:"new"`        // likes not synced before
	NewSaved  int      `json:"new_saved"`  // of those, records newly saved
	Unliked   int      `json:"unliked"`    // full sync only
	Total     int      `json:"total"`      // likes on YouTube
	NewTitles []string `json:"new_titles"` // "Artist - Title" of new likes
}

// Sync pulls likes into the library.
func Sync(ctx context.Context, yt Likes, lib *library.Library, o Options) (Result, error) {
	var res Result
	var all []string // every liked video seen, for spotting unlikes
	full := o.Full
	if n, err := lib.LikeCount(ctx); err == nil && n == 0 {
		full = true // first sync
	}
	token := ""
	for {
		page, err := yt.Likes(ctx, token)
		if err != nil {
			return res, err
		}
		res.Pages++
		res.Total = page.Total
		known, fresh := 0, 0
		var touched []string
		for _, v := range page.Videos {
			res.Checked++
			all = append(all, v.ID)
			had, err := lib.HasLike(ctx, v.ID)
			if err != nil {
				return res, err
			}
			if had {
				known++
				touched = append(touched, v.ID)
				continue
			}
			fresh++
			res.New++
			rec := Record(v)
			res.NewTitles = append(res.NewTitles, rec.Query())
			if o.DryRun {
				continue
			}
			added, err := lib.AddLike(ctx, library.Like{VideoID: v.ID, Title: v.Title, Channel: v.Channel}, rec)
			if err != nil {
				return res, err
			}
			if added {
				res.NewSaved++
			}
		}
		if !o.DryRun {
			if err := lib.TouchLikes(ctx, touched); err != nil {
				return res, err
			}
		}
		if o.Log != nil {
			o.Log("page %d: %d new, %d already synced", res.Pages, fresh, known)
		}
		// Likes come newest first, so a page with nothing new means
		// everything after it was synced before.
		if page.Next == "" || (!full && fresh == 0) {
			break
		}
		token = page.Next
	}
	if full && !o.DryRun {
		n, err := lib.MarkUnliked(ctx, all)
		if err != nil {
			return res, err
		}
		res.Unliked = n
	}
	return res, nil
}

// Record turns a liked video into a record.
func Record(v youtube.LikedVideo) crate.Record {
	artist, title := ParseTitle(v.Title, v.Channel)
	r := crate.Record{
		Artist:           artist,
		Title:            title,
		Source:           "youtube:likes",
		Kind:             "youtube",
		SourceURL:        v.URL(),
		Videos:           []crate.Link{{URL: v.URL(), Title: v.Title, Source: "youtube"}},
		YouTubeSearchURL: youtube.SearchURL(strings.TrimSpace(artist + " " + title)),
		Note:             "liked on YouTube · " + v.Channel,
		TrackOnly:        true,
	}
	return r
}

var (
	// "(Official Video)", "[Official Audio]", "(Lyric Video)", "[HD]", "(Visualiser)"…
	noise = regexp.MustCompile(`(?i)\s*[\(\[][^\)\]]*\b(official|music video|video|audio|lyrics?|visuali[sz]er|hd|hq|4k|full album|out now|premiere)\b[^\)\]]*[\)\]]`)
	// "Artist - Title", with any kind of dash.
	dash  = regexp.MustCompile(`\s+[-–—]\s+`)
	topic = regexp.MustCompile(`\s+-\s+Topic$`)
)

// ParseTitle splits a video title into artist and title.
//
//	"Burial - Archangel (Official Video)", "Hyperdub"  → Burial, Archangel
//	"Archangel", "Burial - Topic"                       → Burial, Archangel
//	"Live at Sonar 2019", "Some Channel"                → "", Live at Sonar 2019
func ParseTitle(title, channel string) (artist, track string) {
	clean := strings.TrimSpace(noise.ReplaceAllString(title, ""))
	if clean == "" {
		clean = strings.TrimSpace(title)
	}
	// YouTube's auto-generated music channels are "<Artist> - Topic" and
	// their video titles are just the track.
	if topic.MatchString(channel) {
		return strings.TrimSpace(topic.ReplaceAllString(channel, "")), clean
	}
	if parts := dash.Split(clean, 2); len(parts) == 2 && parts[0] != "" && parts[1] != "" {
		return strings.TrimSpace(parts[0]), strings.TrimSpace(strings.Trim(parts[1], `"'“”`))
	}
	return "", clean
}
