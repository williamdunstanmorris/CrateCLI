package output

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/williamdunstanmorris/cratecli/internal/crate"
)

// column is one table column. Flexible columns share the space left after
// fixed ones, in proportion to weight, never wider than their content.
type column struct {
	head   string
	cells  []string // plain text
	flex   int      // 0 = fixed width
	right  bool
	width  int
	render func(i int, text string) string // adds colour/links to the fitted text
}

// writeTable prints one row per record, sized to the terminal.
func writeTable(w io.Writer, recs []crate.Record, o Options) error {
	s := style{o}
	n := len(recs)
	cols := []*column{{head: "#", right: true}}
	num := cols[0]
	artist := &column{head: "ARTIST", flex: 3}
	title := &column{head: "TITLE", flex: 4}
	label := &column{head: "LABEL", flex: 2}
	year := &column{head: "YEAR"}
	price := &column{head: "PRICE", right: true}
	source := &column{head: "FROM"}
	listen := &column{head: "LISTEN"}
	note := &column{head: "NOTE", flex: 4}

	var hasLabel, hasYear, hasPrice, hasNote bool
	sources := map[string]bool{}
	sections := map[string]map[string]bool{}
	for _, r := range recs {
		name := sourceName(r.Source)
		if sections[name] == nil {
			sections[name] = map[string]bool{}
		}
		sections[name][r.Source] = true
	}
	for i, r := range recs {
		num.cells = append(num.cells, strconv.Itoa(i+1))
		artist.cells = append(artist.cells, oneLine(crate.CleanArtist(r.Artist)))
		title.cells = append(title.cells, oneLine(r.Title))
		label.cells = append(label.cells, oneLine(r.Label))
		year.cells = append(year.cells, r.Year)
		price.cells = append(price.cells, TidyPrice(r.Price))
		// Just the source's name unless this dig mixes its sections.
		src := sourceName(r.Source)
		if len(sections[src]) > 1 {
			src = r.Source
		}
		source.cells = append(source.cells, src)
		n := r.MyNote
		if n == "" {
			n = r.Note
		}
		note.cells = append(note.cells, oneLine(n))
		hasNote = hasNote || n != ""
		listen.cells = append(listen.cells, listenCell(r))
		hasLabel = hasLabel || r.Label != ""
		hasYear = hasYear || (r.Year != "" && r.Year != "0")
		hasPrice = hasPrice || r.Price != ""
		sources[r.Source] = true
	}

	num.render = func(_ int, t string) string { return s.paint(sgrDim, t) }
	artist.render = func(_ int, t string) string { return s.paint(sgrBold, t) }
	title.render = func(i int, t string) string { return s.link(recs[i].BestURL(), t) }
	label.render = func(i int, t string) string { return s.paint(sgrCyan, s.link(recs[i].DiscogsURL, t)) }
	year.render = func(_ int, t string) string { return s.paint(sgrDim, t) }
	price.render = func(_ int, t string) string { return s.paint(sgrYellow, t) }
	source.render = func(i int, t string) string {
		name := sourceName(recs[i].Source)
		return s.paint(sourceColor(name), s.link(recs[i].SourceURL, t))
	}
	note.render = func(i int, t string) string {
		if recs[i].MyNote != "" {
			return s.paint(sgrMagenta, t)
		}
		return s.paint(sgrDim+";"+sgrItalic, t)
	}
	listen.render = func(i int, t string) string {
		r := recs[i]
		switch {
		case len(r.Videos) > 0:
			return s.paint(sgrGreen, s.link(r.Videos[0].URL, t))
		case r.ListenURL != "" || len(r.Audio) > 0:
			u := r.ListenURL
			if u == "" {
				u = r.Audio[0].URL
			}
			return s.paint(sgrBlue, s.link(u, t))
		}
		return s.paint(sgrDim, s.link(r.YouTubeSearchURL, t))
	}

	cols = append(cols, artist, title)
	narrow := o.Width > 0 && o.Width < 80
	if hasLabel && !narrow {
		cols = append(cols, label)
	}
	if hasYear {
		cols = append(cols, year)
	}
	if hasPrice && !narrow {
		cols = append(cols, price)
	}
	if len(sources) > 1 || (len(recs) > 0 && recs[0].Kind != "" && recs[0].Kind != "database") {
		if !narrow || len(sources) > 1 {
			cols = append(cols, source)
		}
	}
	// Notes only when there's room for them to say something.
	if hasNote && (o.Width == 0 || o.Width >= 140) {
		cols = append(cols, note)
	}
	cols = append(cols, listen)
	fit(cols, o.Width)

	// Header.
	var b strings.Builder
	for j, c := range cols {
		if j > 0 {
			b.WriteString("  ")
		}
		h := truncate(c.head, c.width)
		if c.right {
			b.WriteString(pad(h, c.width) + s.paint(sgrDim, h))
		} else if j == len(cols)-1 {
			b.WriteString(s.paint(sgrDim, h))
		} else {
			b.WriteString(s.paint(sgrDim, h) + pad(h, c.width))
		}
	}
	fmt.Fprintln(w, b.String())

	for i := 0; i < n; i++ {
		b.Reset()
		for j, c := range cols {
			if j > 0 {
				b.WriteString("  ")
			}
			t := truncate(c.cells[i], c.width)
			cell := c.render(i, t)
			switch {
			case c.right:
				b.WriteString(pad(t, c.width) + cell)
			case j == len(cols)-1:
				b.WriteString(cell)
			default:
				b.WriteString(cell + pad(t, c.width))
			}
		}
		fmt.Fprintln(w, b.String())
	}
	return nil
}

// listenCell says how a record can be heard: ▶ videos, ♪ audio, ⌕ search.
func listenCell(r crate.Record) string {
	switch {
	case len(r.Videos) == 1:
		return "▶ video"
	case len(r.Videos) > 1:
		return fmt.Sprintf("▶ %d videos", len(r.Videos))
	case r.ListenURL != "" || len(r.Audio) > 0:
		return "♪ audio"
	}
	return "⌕ search"
}

// fit sets column widths. Fixed columns get their content width; flexible
// ones share what's left of total (0 = unlimited) by weight.
func fit(cols []*column, total int) {
	natural := func(c *column) int {
		w := textWidth(c.head)
		for _, t := range c.cells {
			w = max(w, textWidth(t))
		}
		return w
	}
	var flex []*column
	used := 2 * (len(cols) - 1)
	for _, c := range cols {
		c.width = natural(c)
		if c.flex == 0 {
			used += c.width
		} else {
			flex = append(flex, c)
		}
	}
	if total <= 0 {
		return
	}
	avail := total - used
	need := 0
	for _, c := range flex {
		need += c.width
	}
	if need <= avail {
		return
	}
	const minFlex = 6
	// Hand out space by weight; columns that need less than their share
	// give the rest back to the others.
	open := append([]*column(nil), flex...)
	for _, c := range flex {
		c.width = natural(c)
	}
	for len(open) > 0 {
		weights := 0
		for _, c := range open {
			weights += c.flex
		}
		var still []*column
		settled := false
		for _, c := range open {
			share := avail * c.flex / weights
			if natural(c) <= share {
				c.width = natural(c)
				avail -= c.width
				settled = true
			} else {
				still = append(still, c)
			}
		}
		if !settled {
			for _, c := range still {
				c.width = max(avail*c.flex/weights, minFlex)
			}
			return
		}
		open = still
	}
}

// TidyPrice turns "€ 17.5" into "€17.50" so the column lines up.
func TidyPrice(p string) string {
	p = strings.ReplaceAll(strings.TrimSpace(p), " ", "")
	i := strings.IndexAny(p, "0123456789")
	if i < 0 {
		return p
	}
	sym, num := p[:i], strings.ReplaceAll(p[i:], ",", ".")
	if f, err := strconv.ParseFloat(num, 64); err == nil {
		return fmt.Sprintf("%s%.2f", sym, f)
	}
	return p
}
