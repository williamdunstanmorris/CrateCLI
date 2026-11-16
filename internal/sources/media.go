package sources

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/williamdunstanmorris/cratecli/internal/crate"
)

// ---------------------------------------------------------------- Pitchfork

type pitchfork struct{ env Env }

func (s *pitchfork) Info() Info {
	return Info{
		Name: "pitchfork", Kind: KindPress, Location: "New York", Via: "RSS",
		Homepage:    "https://pitchfork.com",
		Description: "Album reviews, and Best New Albums.",
		Sections: []Section{
			{"reviews", "Latest album reviews"},
			{"best", "Best New Albums"},
		},
	}
}

func (s *pitchfork) Fetch(ctx context.Context, req Request) ([]crate.Record, error) {
	info := s.Info()
	feed := map[string]string{
		"reviews": "https://pitchfork.com/feed/feed-album-reviews/rss",
		"best":    "https://pitchfork.com/feed/reviews/best/albums/rss",
	}[req.Section]
	if feed == "" {
		return nil, sectionError(info, req.Section)
	}
	b, err := s.env.HTTP.Get(ctx, feed)
	if err != nil {
		return nil, err
	}
	items, err := parseRSS(b)
	if err != nil {
		return nil, err
	}
	note := "Pitchfork review"
	if req.Section == "best" {
		note = "Pitchfork Best New Album"
	}
	recs := make([]crate.Record, 0, len(items))
	for _, it := range items {
		recs = append(recs, crate.Record{
			Artist:    pitchforkArtist(it),
			Title:     it.Title,
			Note:      note,
			SourceURL: it.Link,
		})
	}
	return tag(recs, info, req.Section), nil
}

// pitchforkArtist recovers the artist, which the feed leaves out of the
// title. The cover image is usually named "Artist - Title.jpg"; failing
// that, the review URL slug is "<artist>-<title>".
func pitchforkArtist(it rssItem) string {
	if u, err := url.Parse(it.Thumbnail.URL); err == nil {
		base := strings.TrimSuffix(path.Base(u.Path), path.Ext(u.Path))
		// e.g. "Armand Hammer - Snakes & Ladders album art"
		if a, t, ok := strings.Cut(base, " - "); ok && strings.HasPrefix(strings.ToLower(t), strings.ToLower(it.Title)) {
			return strings.TrimSpace(a)
		}
	}
	if u, err := url.Parse(it.Link); err == nil {
		slug := path.Base(strings.TrimSuffix(u.Path, "/"))
		if ts := slugify(it.Title); ts != "" && strings.HasSuffix(slug, "-"+ts) {
			return slugWords(strings.TrimSuffix(slug, "-"+ts))
		}
	}
	return ""
}

// ---------------------------------------------------------------- The Quietus

type quietus struct{ env Env }

func (s *quietus) Info() Info {
	return Info{
		Name: "quietus", Kind: KindPress, Location: "London", Via: "RSS",
		Homepage:    "https://thequietus.com",
		Description: "Independent UK magazine for adventurous music.",
		Sections: []Section{
			{"reviews", "Latest reviews (incl. Album of the Week)"},
			{"album-of-the-week", "Album of the Week only"},
		},
	}
}

var aotwTitle = regexp.MustCompile(`^(?:.*?:\s*)?(.+?) by (.+)$`)

func (s *quietus) Fetch(ctx context.Context, req Request) ([]crate.Record, error) {
	info := s.Info()
	if !info.HasSection(req.Section) {
		return nil, sectionError(info, req.Section)
	}
	want := max(req.Want, 5)
	var recs []crate.Record
	// The feed mixes news and reviews; page back until we have enough reviews.
	for page := 1; page <= 4 && len(recs) < want; page++ {
		u := "https://thequietus.com/feed/"
		if page > 1 {
			u += fmt.Sprintf("?paged=%d", page)
		}
		b, err := s.env.HTTP.Get(ctx, u)
		if err != nil {
			if len(recs) > 0 {
				break
			}
			return nil, err
		}
		items, err := parseRSS(b)
		if err != nil {
			return nil, err
		}
		for _, it := range items {
			isAOTW, isReview := false, false
			for _, c := range it.Categories {
				switch c {
				case "Album of the Week":
					isAOTW = true
				case "Reviews":
					isReview = true
				}
			}
			if !(isAOTW || (isReview && req.Section == "reviews")) {
				continue
			}
			r := crate.Record{SourceURL: it.Link, Note: "The Quietus review"}
			if isAOTW {
				r.Note = "The Quietus Album of the Week"
				if m := aotwTitle.FindStringSubmatch(it.Title); m != nil {
					r.Title, r.Artist = m[1], m[2]
				}
			}
			if r.Title == "" {
				for _, sep := range []string{" – ", " — ", " - "} {
					if a, t, ok := strings.Cut(it.Title, sep); ok {
						r.Artist, r.Title = strings.TrimSpace(a), strings.TrimSpace(t)
						break
					}
				}
			}
			if r.Title == "" {
				continue
			}
			recs = append(recs, r)
		}
	}
	return tag(recs, info, req.Section), nil
}

// ---------------------------------------------------------------- NTS

type nts struct{ env Env }

func (s *nts) Info() Info {
	return Info{
		Name: "nts", Kind: KindRadio, Location: "London", Via: "API (unofficial)",
		Homepage:        "https://www.nts.live",
		Description:     "Tracklists from NTS Radio shows: what DJs actually played.",
		FreeSection:     true,
		FreeSectionName: "show",
		FreeSectionHelp: "a show: the slug from nts.live/shows/<show>",
		Episodes:        true,
	}
}

type ntsEpisode struct {
	Name         string `json:"name"`
	EpisodeAlias string `json:"episode_alias"`
	Broadcast    string `json:"broadcast"`
	Genres       []struct {
		Value string `json:"value"`
	} `json:"genres"`
}

type ntsTrack struct {
	Artist string `json:"artist"`
	Title  string `json:"title"`
}

func (s *nts) Fetch(ctx context.Context, req Request) ([]crate.Record, error) {
	info := s.Info()
	show := strings.Trim(req.Section, "/")
	if show == "" {
		return nil, fmt.Errorf("nts needs a show, e.g. `cratecli from nts the-trilogy-tapes` (the slug from nts.live/shows/<show>)")
	}
	want := max(req.Episodes, 1)
	base := "https://www.nts.live/api/v2/shows/" + url.PathEscape(show)
	// Ask for a few extra: some episodes have no tracklist.
	b, err := s.env.HTTP.Get(ctx, fmt.Sprintf("%s/episodes?offset=0&limit=%d", base, min(want+3, 24)))
	if err != nil {
		return nil, fmt.Errorf("nts show %q: %w", show, err)
	}
	var eps struct {
		Results []ntsEpisode `json:"results"`
	}
	if err := json.Unmarshal(b, &eps); err != nil || len(eps.Results) == 0 {
		return nil, fmt.Errorf("nts: no episodes found for show %q", show)
	}

	var recs []crate.Record
	got := 0
	for _, ep := range eps.Results {
		if got == want {
			break
		}
		b, err := s.env.HTTP.Get(ctx, base+"/episodes/"+url.PathEscape(ep.EpisodeAlias)+"/tracklist")
		if err != nil {
			return nil, err
		}
		var tl struct {
			Results []ntsTrack `json:"results"`
		}
		if err := json.Unmarshal(b, &tl); err != nil {
			return nil, fmt.Errorf("nts tracklist: %w", err)
		}
		tracks := ntsRecords(show, ep, tl.Results)
		if len(tracks) == 0 {
			continue
		}
		recs = append(recs, tracks...)
		got++
	}
	if len(recs) == 0 {
		return nil, fmt.Errorf("nts: the latest %s episodes have no tracklists", show)
	}
	return tag(recs, info, show), nil
}

func ntsRecords(show string, ep ntsEpisode, tracks []ntsTrack) []crate.Record {
	note := episodeNote(strings.TrimSpace(ep.Name), ep.Broadcast, "")
	var genres []string
	for _, g := range ep.Genres {
		genres = append(genres, g.Value)
	}
	var recs []crate.Record
	for _, t := range tracks {
		if strings.TrimSpace(t.Title) == "" {
			continue
		}
		recs = append(recs, crate.Record{
			Artist:    strings.TrimSpace(t.Artist),
			Title:     strings.TrimSpace(t.Title),
			Note:      note,
			Tags:      genres,
			SourceURL: "https://www.nts.live/shows/" + show + "/episodes/" + ep.EpisodeAlias,
			TrackOnly: true,
		})
	}
	return recs
}

// episodeNote is "Show name, 13 Nov 2026" plus the release a track is from.
func episodeNote(name, broadcast, release string) string {
	date := broadcast
	if t, err := time.Parse(time.RFC3339, broadcast); err == nil {
		date = t.Local().Format("2 Jan 2006")
	} else if len(date) >= 10 {
		date = date[:10]
	}
	note := name
	if date != "" {
		note += ", " + date
	}
	if release != "" {
		note = "from “" + release + "” · " + note
	}
	return note
}

// -------------------------------------------------------------------- BBC

type bbc struct{ env Env }

// bbcShows are shortcuts for programme IDs (bbc.co.uk/programmes/<pid>).
var bbcShows = []struct{ name, pid, title, desc string }{
	{"late-junction", "b006tp52", "Late Junction", "Radio 3's Late Junction: adventurous music, ancient to future"},
	{"gilles-peterson", "b01fm4ss", "Gilles Peterson", "Gilles Peterson on 6 Music: soul, jazz, hip hop, house, Afro, Latin"},
}

// dateTitle matches episode titles that are just a date, e.g. "18/12/2026".
var dateTitle = regexp.MustCompile(`^\d{1,2}/\d{1,2}/\d{4}$`)

var bbcPID = regexp.MustCompile(`^[bpmwlh][0-9a-z]{7}$`)

func (s *bbc) Info() Info {
	secs := make([]Section, len(bbcShows))
	for i, sh := range bbcShows {
		secs[i] = Section{sh.name, sh.desc}
	}
	return Info{
		Name: "bbc", Kind: KindRadio, Location: "UK", Via: "BBC programme data",
		Homepage:      "https://www.bbc.co.uk/programmes",
		Description:   "Tracklists (\"Music played\") from BBC radio shows. Any show works by its programme ID.",
		Sections:      secs,
		AcceptSection: func(sec string) bool { return bbcPID.MatchString(sec) },
		Episodes:      true,
	}
}

type bbcProgramme struct {
	Type      string `json:"type"`
	PID       string `json:"pid"`
	Title     string `json:"title"`
	Broadcast string `json:"first_broadcast_date"`
}

type bbcSegment struct {
	Type         string `json:"type"`
	Artist       string `json:"artist"`
	TrackTitle   string `json:"track_title"`
	ReleaseTitle string `json:"release_title"`
	RecordLabel  string `json:"record_label"`
	CatNo        string `json:"catalogue_number"`
}

func (s *bbc) Fetch(ctx context.Context, req Request) ([]crate.Record, error) {
	info := s.Info()
	pid, showName := req.Section, ""
	for _, sh := range bbcShows {
		if sh.name == req.Section {
			pid, showName = sh.pid, sh.title
		}
	}
	if !bbcPID.MatchString(pid) {
		return nil, fmt.Errorf("bbc: %q is neither a known show nor a programme ID (the code in bbc.co.uk/programmes/<id>)", req.Section)
	}
	if showName == "" {
		showName = pid
		if b, err := s.env.HTTP.Get(ctx, "https://www.bbc.co.uk/programmes/"+pid+".json"); err == nil {
			var p struct {
				Programme bbcProgramme `json:"programme"`
			}
			if json.Unmarshal(b, &p) == nil && p.Programme.Title != "" {
				showName = p.Programme.Title
			}
		}
	}
	want := max(req.Episodes, 1)
	b, err := s.env.HTTP.Get(ctx, fmt.Sprintf("https://www.bbc.co.uk/programmes/%s/children.json?limit=%d", pid, min(want+6, 30)))
	if err != nil {
		return nil, fmt.Errorf("bbc %s: %w", showName, err)
	}
	eps, err := bbcEpisodes(b, time.Now())
	if err != nil {
		return nil, err
	}
	if len(eps) == 0 {
		return nil, fmt.Errorf("bbc: no broadcast episodes found for %s", showName)
	}

	var recs []crate.Record
	got := 0
	for _, ep := range eps {
		if got == want {
			break
		}
		b, err := s.env.HTTP.Get(ctx, "https://www.bbc.co.uk/programmes/"+ep.PID+"/segments.json")
		if err != nil {
			// Episodes without a tracklist have no segments page.
			continue
		}
		tracks, err := bbcRecords(b, showName, ep)
		if err != nil {
			return nil, err
		}
		if len(tracks) == 0 {
			continue
		}
		recs = append(recs, tracks...)
		got++
	}
	if len(recs) == 0 {
		return nil, fmt.Errorf("bbc: the latest %s episodes have no tracklists yet", showName)
	}
	return tag(recs, info, req.Section), nil
}

// bbcEpisodes lists episodes already broadcast, newest first. The BBC
// lists scheduled future episodes too.
func bbcEpisodes(b []byte, now time.Time) ([]bbcProgramme, error) {
	var resp struct {
		Children struct {
			Programmes []bbcProgramme `json:"programmes"`
		} `json:"children"`
	}
	if err := json.Unmarshal(b, &resp); err != nil {
		return nil, fmt.Errorf("bbc: %w", err)
	}
	var out []bbcProgramme
	for _, p := range resp.Children.Programmes {
		if p.Type != "episode" {
			continue
		}
		if t, err := time.Parse(time.RFC3339, p.Broadcast); err == nil && t.After(now) {
			continue
		}
		out = append(out, p)
	}
	return out, nil
}

func bbcRecords(b []byte, show string, ep bbcProgramme) ([]crate.Record, error) {
	name := show
	if ep.Title != "" && !dateTitle.MatchString(ep.Title) && ep.Title != show {
		name += ": " + ep.Title
	}
	var resp struct {
		Events []struct {
			Segment bbcSegment `json:"segment"`
		} `json:"segment_events"`
	}
	if err := json.Unmarshal(b, &resp); err != nil {
		return nil, fmt.Errorf("bbc tracklist: %w", err)
	}
	var recs []crate.Record
	for _, e := range resp.Events {
		g := e.Segment
		if g.Type != "music" || strings.TrimSpace(g.TrackTitle) == "" {
			continue
		}
		recs = append(recs, crate.Record{
			Artist:    strings.TrimSpace(g.Artist),
			Title:     strings.TrimSpace(g.TrackTitle),
			Label:     strings.TrimSpace(g.RecordLabel),
			CatNo:     strings.TrimSpace(g.CatNo),
			Note:      episodeNote(name, ep.Broadcast, strings.TrimSpace(g.ReleaseTitle)),
			SourceURL: "https://www.bbc.co.uk/programmes/" + ep.PID,
			TrackOnly: true,
		})
	}
	return recs, nil
}

// ---------------------------------------------------------------- Bandcamp

type bandcamp struct{ env Env }

func (s *bandcamp) Info() Info {
	return Info{
		Name: "bandcamp", Kind: KindMixed, Location: "online", Via: "API (unofficial)",
		Homepage:    "https://bandcamp.com/discover",
		Description: "Bandcamp Discover by tag. Includes a stream of each release's featured track.",
		Sections: []Section{
			{"new", "Newest releases"},
			{"top", "Best-selling"},
			{"rand", "Random picks"},
		},
		Tags: true,
	}
}

func (s *bandcamp) Fetch(ctx context.Context, req Request) ([]crate.Record, error) {
	info := s.Info()
	if !info.HasSection(req.Section) {
		return nil, sectionError(info, req.Section)
	}
	tags := make([]string, 0, len(req.Tags))
	for _, t := range req.Tags {
		if t = slugify(t); t != "" {
			tags = append(tags, t)
		}
	}
	body, _ := json.Marshal(map[string]any{
		"category_id": 0, "tag_norm_names": tags, "geoname_id": 0,
		"slice": req.Section, "cursor": "*", "size": min(max(req.Want*2, 24), 60),
		"include_result_types": []string{"a", "s"},
	})
	b, err := s.env.HTTP.PostJSON(ctx, "https://bandcamp.com/api/discover/1/discover_web", body)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Results []struct {
			Title        string `json:"title"`
			AlbumArtist  string `json:"albumArtist"`
			BandName     string `json:"bandName"`
			BandLocation string `json:"bandLocation"`
			ItemURL      string `json:"itemUrl"`
			ReleaseDate  string `json:"releaseDate"`
			Price        struct {
				Amount   float64 `json:"amount"`
				Currency string  `json:"currency"`
			} `json:"price"`
			PlayerData struct {
				Title     string `json:"title"`
				StreamURL string `json:"streamUrl"`
			} `json:"playerData"`
		} `json:"results"`
	}
	if err := json.Unmarshal(b, &resp); err != nil {
		return nil, fmt.Errorf("bandcamp: %w", err)
	}
	recs := make([]crate.Record, 0, len(resp.Results))
	for _, x := range resp.Results {
		r := crate.Record{
			Artist: x.BandName,
			Title:  x.Title,
			Tags:   tags,
		}
		// albumArtist is set when a label page releases another artist's work.
		if x.AlbumArtist != "" {
			r.Artist, r.Label = x.AlbumArtist, x.BandName
		}
		if u, err := url.Parse(x.ItemURL); err == nil {
			u.RawQuery = ""
			r.SourceURL, r.ListenURL = u.String(), u.String()
		}
		if len(x.ReleaseDate) >= 10 {
			r.Year = x.ReleaseDate[:4]
			r.Note = "Released " + x.ReleaseDate[:10]
		}
		if x.BandLocation != "" {
			r.Note = strings.TrimPrefix(r.Note+" · "+x.BandLocation, " · ")
		}
		if x.Price.Amount > 0 {
			r.Price = fmt.Sprintf("%.2f %s", x.Price.Amount/100, x.Price.Currency)
		}
		if x.PlayerData.StreamURL != "" {
			r.Audio = []crate.Link{{URL: x.PlayerData.StreamURL, Title: r.Artist + " - " + x.PlayerData.Title, Source: "bandcamp"}}
		}
		recs = append(recs, r)
	}
	return tag(recs, info, req.Section), nil
}
