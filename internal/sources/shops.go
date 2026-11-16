package sources

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/PuerkitoBio/goquery"

	"github.com/williamdunstanmorris/cratecli/internal/crate"
)

// ---------------------------------------------------------------- Hard Wax

type hardWax struct{ env Env }

func (s *hardWax) Info() Info {
	return Info{
		Name: "hardwax", Kind: KindShop, Location: "Berlin", Via: "web page",
		Homepage:    "https://hardwax.com",
		Description: "Berlin's dub techno institution. Every record comes with a terse one-line verdict.",
		Sections: []Section{
			{"this-week", "New arrivals this week"},
			{"last-week", "Last week's arrivals"},
			{"essentials", "The shop's canon"},
			{"back-in-stock", "Restocked"},
			{"reissues", "Reissues"},
			{"exclusives", "Hard Wax exclusives"},
			{"collectors-items", "Rare and second-hand"},
			{"techno", "Techno"}, {"house", "House"}, {"electro", "Electro"},
			{"detroit", "Detroit"}, {"detroit-house", "Detroit house"},
			{"chicago-oldschool", "Chicago old school"}, {"disco", "Disco"},
			{"ambient", "Ambient"}, {"electronica", "Electronica"}, {"bass", "Bass"},
			{"drum-n-bass", "Drum & bass"}, {"grime", "Grime"},
			{"new-global-styles", "New global styles"},
			{"basic-channel", "Basic Channel family"}, {"drexciya", "Drexciya family"},
			{"honest-jons", "Honest Jon's label"},
		},
	}
}

var hwID = regexp.MustCompile(`^record-(\d+)$`)
var hwFormat = regexp.MustCompile(`\(([^)]+)\) to your order`)

func (s *hardWax) Fetch(ctx context.Context, req Request) ([]crate.Record, error) {
	info := s.Info()
	if !info.HasSection(req.Section) {
		return nil, sectionError(info, req.Section)
	}
	page, err := s.env.HTTP.Get(ctx, "https://hardwax.com/"+req.Section+"/")
	if err != nil {
		return nil, err
	}
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(page))
	if err != nil {
		return nil, err
	}

	var recs []crate.Record
	// Class names on hardwax.com are minified and change between deploys,
	// so select by structure: record-NNNNN containers, /act/ and /label/ links.
	doc.Find(`div[id^="record-"]`).Each(func(_ int, rec *goquery.Selection) {
		id, _ := rec.Attr("id")
		m := hwID.FindStringSubmatch(id)
		if m == nil {
			return
		}
		heading := strings.Join(strings.Fields(rec.Find("h2").First().Text()), " ")
		artist, title, ok := strings.Cut(heading, ": ")
		if !ok {
			artist, title = "", heading
		}
		label := strings.TrimSpace(rec.Find(`a[href^="/label/"]`).First().Text())
		note := strings.TrimSpace(rec.Find("h2").First().NextFiltered("p").Text())
		if note == "" {
			note = strings.TrimSpace(rec.Find("p").First().Text())
		}
		r := crate.Record{
			Artist:    strings.TrimSpace(artist),
			Title:     strings.TrimSpace(title),
			Label:     label,
			Note:      note,
			SourceURL: fmt.Sprintf("https://hardwax.com/%s/", m[1]),
			Tags:      []string{req.Section},
		}
		// Hard Wax shows only part of the catalogue number ("090" for
		// BP090), which would mis-match on Discogs, so match on
		// artist/title/label instead.
		if href, ok := rec.Find(`a[href^="/` + m[1] + `/"]`).First().Attr("href"); ok {
			r.SourceURL = "https://hardwax.com" + href
		}
		if t, ok := rec.Find(`div[id^="item-"] a[title]`).First().Attr("title"); ok {
			if fm := hwFormat.FindStringSubmatch(t); fm != nil {
				r.Format = fm[1]
			}
		}
		price := strings.TrimSpace(rec.Find(`div[id^="item-"] a`).First().Find("span").Last().Text())
		if strings.Contains(price, "€") {
			r.Price = price
		}
		if r.Title != "" {
			recs = append(recs, r)
		}
	})
	if len(recs) == 0 {
		return nil, fmt.Errorf("hardwax: found no records on %s (layout may have changed)", req.Section)
	}
	return tag(recs, info, req.Section), nil
}

// ---------------------------------------------------------------- OYE

type oye struct{ env Env }

func (s *oye) Info() Info {
	return Info{
		Name: "oye", Kind: KindShop, Location: "Berlin", Via: "web page",
		Homepage:    "https://oye-records.com",
		Description: "House, disco, hip hop, jazz and beyond, since 2002. Staff write-ups on every release.",
		Sections: []Section{
			{"new", "New in stock (100 latest)"},
			{"upcoming", "Pre-orders and upcoming releases"},
			{"used", "Used records, latest in"},
			{"bestsellers", "This month's bestsellers"},
		},
	}
}

var discogsReleaseURL = regexp.MustCompile(`/release/(\d+)`)

func (s *oye) Fetch(ctx context.Context, req Request) ([]crate.Record, error) {
	info := s.Info()
	path := map[string]string{
		"new": "/releases/new", "upcoming": "/releases/upcoming",
		"used": "/releases/used", "bestsellers": "/charts",
	}[req.Section]
	if path == "" {
		return nil, sectionError(info, req.Section)
	}
	page, err := s.env.HTTP.Get(ctx, "https://oye-records.com"+path)
	if err != nil {
		return nil, err
	}
	data, err := nuxtData(page)
	if err != nil {
		return nil, fmt.Errorf("oye: %w", err)
	}
	edges := findEdges(data)
	if len(edges) == 0 {
		return nil, fmt.Errorf("oye: no releases found on %s (layout may have changed)", req.Section)
	}

	recs := make([]crate.Record, 0, len(edges))
	for _, e := range edges {
		n := obj(e, "node")
		if n == nil || str(n, "title") == "" {
			continue
		}
		r := crate.Record{
			Artist:    str(n, "name"),
			Title:     str(n, "title"),
			Label:     str(n, "label"),
			Format:    str(n, "format"),
			Note:      firstSentence(str(n, "description"), 180),
			SourceURL: "https://oye-records.com/releases/" + str(n, "slug"),
		}
		for _, g := range []string{str(obj(n, "mainGenre"), "name"), str(obj(n, "mainGenreSub"), "name")} {
			if g != "" && g != "tba" && (len(r.Tags) == 0 || r.Tags[0] != g) {
				r.Tags = append(r.Tags, g)
			}
		}
		if r.Label == "" {
			// Chart entries carry the label on their tracks' release.
			if t := list(n, "tracks"); len(t) > 0 {
				r.Label = str(obj(t[0], "release"), "label")
			}
		}
		if p := obj(n, "price"); p != nil && str(p, "gross") != "" {
			r.Price = str(p, "gross") + " " + str(p, "currency")
		}
		if m := discogsReleaseURL.FindStringSubmatch(str(n, "discogsUrl")); m != nil {
			r.ReleaseID, _ = strconv.Atoi(m[1])
			r.DiscogsURL = "https://www.discogs.com/release/" + m[1]
		}
		for _, t := range list(n, "tracks") {
			if u := str(obj(t), "url"); u != "" {
				r.Audio = append(r.Audio, crate.Link{URL: u, Title: r.Artist + " - " + r.Title, Source: "oye"})
			}
		}
		recs = append(recs, r)
	}
	return tag(recs, info, req.Section), nil
}

// findEdges returns the first GraphQL-style edges list whose nodes are releases.
func findEdges(v any) []any {
	switch t := v.(type) {
	case map[string]any:
		if e, ok := t["edges"].([]any); ok && len(e) > 0 {
			if n := obj(e[0], "node"); n != nil && n["title"] != nil {
				return e
			}
		}
		for _, k := range []string{"newReleases", "upcomingReleases", "usedReleases", "charts", "bestsellers", "releases"} {
			if x, ok := t[k]; ok {
				if e := findEdges(x); e != nil {
					return e
				}
			}
		}
		for _, x := range t {
			if e := findEdges(x); e != nil {
				return e
			}
		}
	case []any:
		for _, x := range t {
			if e := findEdges(x); e != nil {
				return e
			}
		}
	}
	return nil
}

// ---------------------------------------------------------------- Clone

type clone struct{ env Env }

func (s *clone) Info() Info {
	return Info{
		Name: "clone", Kind: KindShop, Location: "Rotterdam", Via: "RSS",
		Homepage:    "https://clone.nl",
		Description: "Electro, techno, Italo and Dutch dance music; distributor and shop.",
		Sections:    []Section{{"new", "Latest 100 arrivals"}},
	}
}

func (s *clone) Fetch(ctx context.Context, req Request) ([]crate.Record, error) {
	info := s.Info()
	if !info.HasSection(req.Section) {
		return nil, sectionError(info, req.Section)
	}
	b, err := s.env.HTTP.Get(ctx, "https://clone.nl/rss/all")
	if err != nil {
		return nil, err
	}
	items, err := parseRSS(b)
	if err != nil {
		return nil, err
	}
	recs := make([]crate.Record, 0, len(items))
	for _, it := range items {
		artist, title, format, catno := splitCloneTitle(it.Title)
		if title == "" {
			continue
		}
		recs = append(recs, crate.Record{
			Artist: artist, Title: title, Format: format, CatNo: catno,
			Note:      firstSentence(stripTags(it.Content), 180),
			SourceURL: it.Link,
		})
	}
	return tag(recs, info, req.Section), nil
}

var trailingParen = regexp.MustCompile(`\s*\(([^()]*)\)\s*$`)

// splitCloneTitle parses "Artist - Title (Edition) (3x12inch) - CATNO".
func splitCloneTitle(s string) (artist, title, format, catno string) {
	s = strings.Join(strings.Fields(s), " ")
	if i := strings.LastIndex(s, " - "); i > 0 {
		rest := s[i+3:]
		if !strings.Contains(rest, " ") || len(rest) <= 16 {
			catno, s = rest, s[:i]
		}
	}
	artist, title, ok := strings.Cut(s, " - ")
	if !ok {
		artist, title = "", s
	}
	if m := trailingParen.FindStringSubmatch(title); m != nil {
		if strings.Contains(strings.ToLower(m[1]), "inch") || strings.Contains(m[1], "LP") || strings.Contains(m[1], "CD") {
			format = m[1]
			title = strings.TrimSpace(title[:len(title)-len(m[0])])
		}
	}
	return strings.TrimSpace(artist), strings.TrimSpace(title), format, catno
}

// ---------------------------------------------------------------- Honest Jon's

type honestJons struct{ env Env }

var hjCategories = map[string]string{
	"blues": "Blues", "dance": "Dance", "folk": "Folk", "jazz": "Jazz", "odds": "Odds",
	"outernational": "Outernational", "reggae": "Reggae", "soul-funk": "Soul_Funk",
}

func (s *honestJons) Info() Info {
	return Info{
		Name: "honestjons", Kind: KindShop, Location: "London", Via: "web page",
		Homepage:    "https://honestjons.com",
		Description: "Portobello Road since 1974: reggae, jazz, soul, African and \"outernational\" music.",
		Sections: []Section{
			{"latest", "Latest 100 arrivals"},
			{"reggae", "Reggae"}, {"jazz", "Jazz"}, {"outernational", "Outernational"},
			{"soul-funk", "Soul / Funk"}, {"dance", "Dance"}, {"blues", "Blues"},
			{"folk", "Folk"}, {"odds", "Odds"},
		},
	}
}

func (s *honestJons) Fetch(ctx context.Context, req Request) ([]crate.Record, error) {
	info := s.Info()
	var urls []string
	switch {
	case req.Section == "latest":
		// 25 per page, 4 pages. Fetch only as many pages as needed.
		pages := (req.Want*2)/25 + 1
		if req.Want == 0 || pages > 4 {
			pages = 4
		}
		urls = append(urls, "https://honestjons.com/shop/latest_100_arrivals")
		for p := 2; p <= pages; p++ {
			urls = append(urls, fmt.Sprintf("https://honestjons.com/shop/latest_100_arrivals/%d", p))
		}
	case hjCategories[req.Section] != "":
		urls = []string{"https://honestjons.com/shop/category/" + hjCategories[req.Section]}
	default:
		return nil, sectionError(info, req.Section)
	}

	seen := map[string]bool{}
	var recs []crate.Record
	for _, u := range urls {
		page, err := s.env.HTTP.Get(ctx, u)
		if err != nil {
			if len(recs) > 0 {
				break
			}
			return nil, err
		}
		doc, err := goquery.NewDocumentFromReader(bytes.NewReader(page))
		if err != nil {
			return nil, err
		}
		doc.Find("h2").Each(func(_ int, h *goquery.Selection) {
			a := h.Find(`a[href*="/release/"]`).First()
			href, ok := a.Attr("href")
			if !ok || seen[href] {
				return
			}
			box := h.Parent()
			title := strings.TrimSpace(box.Find("h3").First().Text())
			if title == "" {
				return
			}
			seen[href] = true
			recs = append(recs, crate.Record{
				Artist:    strings.TrimSpace(a.Text()),
				Title:     title,
				Label:     strings.TrimSpace(box.Find("h4").First().Text()),
				SourceURL: href,
				Tags:      []string{req.Section},
			})
		})
	}
	if len(recs) == 0 {
		return nil, fmt.Errorf("honestjons: found no records (layout may have changed)")
	}
	return tag(recs, info, req.Section), nil
}

// ---------------------------------------------------------------- Rush Hour

type rushHour struct{ env Env }

func (s *rushHour) Info() Info {
	return Info{
		Name: "rushhour", Kind: KindShop, Location: "Amsterdam", Via: "web page + Discogs",
		Homepage:    "https://www.rushhour.nl",
		Description: "Amsterdam shop, label and distributor: house, techno, jazz, Detroit.",
		Sections: []Section{
			{"new", "New this week (website)"},
			{"stock", "Latest Discogs listings (seller: rushhour)"},
		},
	}
}

var rhCatalog = regexp.MustCompile(`^(.*?)\s*\(([^()]+)\)\s*$`)

func (s *rushHour) Fetch(ctx context.Context, req Request) ([]crate.Record, error) {
	info := s.Info()
	switch req.Section {
	case "stock":
		recs, err := crate.FromSeller(ctx, s.env.Discogs, "rushhour", crate.SellerOptions{Limit: max(req.Want, 25)})
		return tag(recs, info, req.Section), err
	case "new":
	default:
		return nil, sectionError(info, req.Section)
	}
	page, err := s.env.HTTP.Get(ctx, "https://www.rushhour.nl/new-this-week")
	if err != nil {
		return nil, err
	}
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(page))
	if err != nil {
		return nil, err
	}
	var recs []crate.Record
	doc.Find(".node-record").Each(func(_ int, n *goquery.Selection) {
		field := func(sel string) string { return strings.TrimSpace(n.Find(sel).First().Text()) }
		r := crate.Record{
			Artist: unshout(field(".field-name-field-artist")),
			Title:  unshout(field(".field-name-title")),
			Label:  unshout(field(".field-name-field-label")),
		}
		if m := rhCatalog.FindStringSubmatch(field(".group-format-catalog")); m != nil {
			r.Format, r.CatNo = m[1], m[2]
		}
		if g := strings.TrimPrefix(field(".field-name-field-genre"), "Tag: "); g != "" {
			r.Tags = strings.Split(g, " - ")
		}
		if href, ok := n.Find("a.link-overlay").Attr("href"); ok {
			r.SourceURL = "https://www.rushhour.nl" + href
		}
		if r.Title != "" {
			recs = append(recs, r)
		}
	})
	if len(recs) == 0 {
		return nil, fmt.Errorf("rushhour: found no records (layout may have changed)")
	}
	return tag(recs, info, req.Section), nil
}

// unshout turns "JUAN ATKINS" into "Juan Atkins" but leaves mixed case alone.
func unshout(s string) string {
	if s == "" || strings.ToUpper(s) != s {
		return s
	}
	words := strings.Fields(strings.ToLower(s))
	for i, w := range words {
		r := []rune(w)
		for j := range r {
			if unicode.IsLetter(r[j]) {
				r[j] = unicode.ToUpper(r[j])
				break
			}
		}
		words[i] = string(r)
	}
	return strings.Join(words, " ")
}

// ---------------------------------------------------------------- Discogs-backed shops

type discogsShop struct {
	env      Env
	name     string
	username string
	location string
	desc     string
}

func newDiscogsShop(env Env, name, username, location, desc string) *discogsShop {
	return &discogsShop{env, name, username, location, desc}
}

func (s *discogsShop) Info() Info {
	return Info{
		Name: s.name, Kind: KindShop, Location: s.location, Via: "Discogs",
		Homepage:    "https://www.discogs.com/seller/" + s.username + "/profile",
		Description: s.desc,
		Sections:    []Section{{"new", "Latest Discogs listings (seller: " + s.username + ")"}},
	}
}

func (s *discogsShop) Fetch(ctx context.Context, req Request) ([]crate.Record, error) {
	info := s.Info()
	if !info.HasSection(req.Section) {
		return nil, sectionError(info, req.Section)
	}
	recs, err := crate.FromSeller(ctx, s.env.Discogs, s.username, crate.SellerOptions{Limit: max(req.Want, 25)})
	return tag(recs, info, req.Section), err
}
