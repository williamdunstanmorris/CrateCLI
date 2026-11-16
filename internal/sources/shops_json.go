package sources

// Shops whose own storefront software publishes product data as JSON:
// Yoyaku (WordPress/WooCommerce) and Rubadub (Shopify). These are the same
// public endpoints the shops' websites and search engines read; no login.

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"strconv"
	"strings"

	"github.com/williamdunstanmorris/cratecli/internal/crate"
)

// ---------------------------------------------------------------- Yoyaku

type yoyaku struct{ env Env }

// yoyakuCategories maps sections to WooCommerce product category IDs
// (from /wp-json/wc/store/v1/products/categories).
var yoyakuCategories = map[string]int{
	"arrivals":        12538,
	"forthcoming":     4044,
	"back-in-stock":   8018,
	"warehouse-finds": 30150,
	"exclusive":       4042,
	"yoyaku-records":  6900,
}

func (s *yoyaku) Info() Info {
	return Info{
		Name: "yoyaku", Kind: KindShop, Location: "Paris", Via: "shop API (WordPress)",
		Homepage:    "https://yoyaku.io",
		Description: "Paris shop and distributor for minimal, deep house and techno, with its own labels.",
		Sections: []Section{
			{"arrivals", "Newest arrivals"},
			{"forthcoming", "Upcoming releases"},
			{"back-in-stock", "Restocked records"},
			{"warehouse-finds", "Older stock dug out of the warehouse"},
			{"exclusive", "Yoyaku exclusives"},
			{"yoyaku-records", "Yoyaku's own label"},
		},
	}
}

func (s *yoyaku) Fetch(ctx context.Context, req Request) ([]crate.Record, error) {
	info := s.Info()
	cat, ok := yoyakuCategories[req.Section]
	if !ok {
		return nil, sectionError(info, req.Section)
	}
	perPage := min(max(req.Want, 30), 100)
	// WordPress gives artist, label, styles and year as taxonomy terms...
	wp, err := s.env.HTTP.Get(ctx, fmt.Sprintf(
		"https://yoyaku.io/wp-json/wp/v2/product?product_cat=%d&per_page=%d&_embed=wp:term", cat, perPage))
	if err != nil {
		return nil, err
	}
	recs, ids, err := parseYoyakuWP(wp)
	if err != nil {
		return nil, err
	}
	if len(recs) == 0 {
		return nil, fmt.Errorf("yoyaku: nothing in %s right now", req.Section)
	}
	// ...and the store API adds catalogue number and price.
	strIDs := make([]string, len(ids))
	for i, id := range ids {
		strIDs[i] = strconv.Itoa(id)
	}
	store, err := s.env.HTTP.Get(ctx, fmt.Sprintf(
		"https://yoyaku.io/wp-json/wc/store/v1/products?include=%s&per_page=%d",
		url.QueryEscape(strings.Join(strIDs, ",")), len(ids)))
	if err == nil {
		err = addYoyakuStore(store, recs, ids)
	}
	if err != nil {
		// Still useful without prices.
		if s.env.Log != nil {
			s.env.Log("warn: yoyaku prices: %v", err)
		}
	}
	return tag(recs, info, req.Section), nil
}

type wpTerm struct {
	Taxonomy string `json:"taxonomy"`
	Name     string `json:"name"`
}

type wpProduct struct {
	ID       int                       `json:"id"`
	Link     string                    `json:"link"`
	Title    struct{ Rendered string } `json:"title"`
	Excerpt  struct{ Rendered string } `json:"excerpt"`
	Embedded struct {
		Terms [][]wpTerm `json:"wp:term"`
	} `json:"_embedded"`
}

// parseYoyakuWP reads /wp/v2/product?_embed=wp:term. It returns records
// in the same order as the product IDs.
func parseYoyakuWP(b []byte) ([]crate.Record, []int, error) {
	var ps []wpProduct
	if err := json.Unmarshal(b, &ps); err != nil {
		return nil, nil, fmt.Errorf("yoyaku: %w", err)
	}
	recs := make([]crate.Record, 0, len(ps))
	ids := make([]int, 0, len(ps))
	for _, p := range ps {
		terms := map[string][]string{}
		for _, group := range p.Embedded.Terms {
			for _, t := range group {
				terms[t.Taxonomy] = append(terms[t.Taxonomy], html.UnescapeString(t.Name))
			}
		}
		r := crate.Record{
			Artist:    joinArtists(terms["musicartist"]),
			Title:     html.UnescapeString(strings.TrimSpace(p.Title.Rendered)),
			Tags:      terms["musicstyle"],
			SourceURL: p.Link,
			Note:      firstSentence(stripTags(p.Excerpt.Rendered), 180),
		}
		if l := terms["musiclabel"]; len(l) > 0 {
			r.Label = l[0]
		}
		if y := terms["musicyear"]; len(y) > 0 {
			r.Year = y[0]
		}
		if c := terms["musiccountry"]; len(c) > 0 {
			r.Country = c[0]
		}
		if r.Title == "" {
			continue
		}
		recs = append(recs, r)
		ids = append(ids, p.ID)
	}
	return recs, ids, nil
}

type wcProduct struct {
	ID     int    `json:"id"`
	SKU    string `json:"sku"`
	Prices struct {
		Price        string `json:"price"`
		CurrencyCode string `json:"currency_code"`
		MinorUnit    int    `json:"currency_minor_unit"`
	} `json:"prices"`
}

// addYoyakuStore fills catalogue number and price from the store API.
func addYoyakuStore(b []byte, recs []crate.Record, ids []int) error {
	var ps []wcProduct
	if err := json.Unmarshal(b, &ps); err != nil {
		return err
	}
	byID := make(map[int]wcProduct, len(ps))
	for _, p := range ps {
		byID[p.ID] = p
	}
	for i, id := range ids {
		p, ok := byID[id]
		if !ok {
			continue
		}
		recs[i].CatNo = p.SKU
		recs[i].Price = formatPrice(p.Prices.Price, p.Prices.MinorUnit, p.Prices.CurrencyCode)
	}
	return nil
}

// formatPrice turns ("1500", 2, "EUR") into "€ 15.00".
func formatPrice(minor string, unit int, currency string) string {
	n, err := strconv.Atoi(minor)
	if err != nil || n == 0 {
		return ""
	}
	sym := map[string]string{"EUR": "€", "GBP": "£", "USD": "$", "JPY": "¥"}[currency]
	if sym == "" {
		sym = currency
	}
	v := float64(n)
	for range unit {
		v /= 10
	}
	return fmt.Sprintf("%s %.*f", sym, unit, v)
}

// joinArtists credits several artists the way Discogs does.
func joinArtists(names []string) string {
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	case 2:
		return names[0] + " & " + names[1]
	}
	return strings.Join(names[:len(names)-1], ", ") + " & " + names[len(names)-1]
}

// --------------------------------------------------------------- Rubadub

type rubadub struct{ env Env }

// rubadubCollections maps sections to Shopify collection handles. The shop
// also sells synths and DJ gear; only products of type "Records" are kept.
var rubadubCollections = []struct{ section, handle, desc string }{
	{"new", "records-new-arrivals", "New arrivals"},
	{"recommended", "record-store-recommendations", "The shop's recommendations"},
	{"back-in-stock", "back-in-stock", "Back in stock and represses"},
	{"pre-orders", "pre-orders", "Pre-orders"},
	{"exclusive", "exclusive", "Rubadub exclusives and distribution"},
	{"sale", "record-sale", "Records on sale"},
	{"house", "house", "House"},
	{"techno", "techno-electro", "Techno and electro"},
	{"experimental", "electronic-experimental", "Electronic and experimental"},
	{"disco", "disco-edits", "Disco and edits"},
	{"reggae", "reggae", "Reggae"},
	{"hip-hop", "hip-hop-rap", "Hip hop and rap"},
	{"bass", "grime-bass", "Grime and bass"},
	{"funk-soul", "funk-soul", "Funk and soul"},
	{"classic-house", "classic-house", "Classic house"},
	{"classic-techno", "classic-techno", "Classic techno"},
}

func (s *rubadub) Info() Info {
	secs := make([]Section, len(rubadubCollections))
	for i, c := range rubadubCollections {
		secs[i] = Section{c.section, c.desc}
	}
	return Info{
		Name: "rubadub", Kind: KindShop, Location: "Glasgow", Via: "shop API (Shopify)",
		Homepage:    "https://rubadub.co.uk",
		Description: "Glasgow record shop and distributor since 1992: house, techno, dub, disco.",
		Sections:    secs,
	}
}

func (s *rubadub) Fetch(ctx context.Context, req Request) ([]crate.Record, error) {
	info := s.Info()
	handle := ""
	for _, c := range rubadubCollections {
		if c.section == req.Section {
			handle = c.handle
		}
	}
	if handle == "" {
		return nil, sectionError(info, req.Section)
	}
	limit := min(max(req.Want*2, 50), 250)
	b, err := s.env.HTTP.Get(ctx, fmt.Sprintf("https://rubadub.co.uk/collections/%s/products.json?limit=%d", handle, limit))
	if err != nil {
		return nil, err
	}
	recs, err := parseShopify(b, "https://rubadub.co.uk", "£")
	if err != nil {
		return nil, fmt.Errorf("rubadub: %w", err)
	}
	if len(recs) == 0 {
		return nil, fmt.Errorf("rubadub: no records in %s right now", req.Section)
	}
	return tag(recs, info, req.Section), nil
}

type shopifyProduct struct {
	Title       string   `json:"title"`
	Handle      string   `json:"handle"`
	BodyHTML    string   `json:"body_html"`
	Vendor      string   `json:"vendor"`
	ProductType string   `json:"product_type"`
	Tags        []string `json:"tags"`
	Variants    []struct {
		Price string `json:"price"`
		SKU   string `json:"sku"`
	} `json:"variants"`
}

// parseShopify reads a Shopify products.json whose tags carry
// "Artist|…", "Label|…", "Genre|…" and "Format|…" facets.
func parseShopify(b []byte, base, currency string) ([]crate.Record, error) {
	var resp struct {
		Products []shopifyProduct `json:"products"`
	}
	if err := json.Unmarshal(b, &resp); err != nil {
		return nil, err
	}
	var recs []crate.Record
	for _, p := range resp.Products {
		if !strings.EqualFold(p.ProductType, "Records") {
			continue
		}
		facets := map[string][]string{}
		for _, t := range p.Tags {
			if k, v, ok := strings.Cut(t, "|"); ok && v != "" {
				facets[k] = append(facets[k], v)
			}
		}
		r := crate.Record{
			Artist:    joinArtists(facets["Artist"]),
			Title:     strings.TrimSpace(html.UnescapeString(p.Title)),
			Label:     p.Vendor,
			Format:    strings.Join(facets["Format"], ", "),
			Tags:      facets["Genre"],
			SourceURL: base + "/products/" + p.Handle,
			Note:      firstSentence(stripTags(p.BodyHTML), 180),
		}
		if l := facets["Label"]; len(l) > 0 && r.Label == "" {
			r.Label = l[0]
		}
		if len(p.Variants) > 0 {
			v := p.Variants[0]
			r.CatNo = strings.TrimSpace(v.SKU)
			if v.Price != "" && v.Price != "0.00" {
				r.Price = currency + " " + v.Price
			}
		}
		recs = append(recs, r)
	}
	return recs, nil
}
