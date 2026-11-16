package output

import (
	"strings"
	"unicode"

	"golang.org/x/text/width"
)

// Options controls terminal niceties for the table and long formats.
// The zero value is plain text with no width limit, safe for pipes.
type Options struct {
	Color bool // ANSI colours
	Links bool // OSC 8 hyperlinks (clickable text in Ghostty, iTerm2, WezTerm…)
	Width int  // terminal columns; 0 = don't truncate
}

type style struct{ Options }

// ANSI SGR codes; the 16 basic colours follow the user's terminal theme.
const (
	sgrBold    = "1"
	sgrDim     = "2"
	sgrItalic  = "3"
	sgrRed     = "31"
	sgrGreen   = "32"
	sgrYellow  = "33"
	sgrBlue    = "34"
	sgrMagenta = "35"
	sgrCyan    = "36"
)

func (s style) paint(code, text string) string {
	if !s.Color || text == "" || code == "" {
		return text
	}
	return "\x1b[" + code + "m" + text + "\x1b[0m"
}

func (s style) link(url, text string) string {
	if !s.Links || url == "" || text == "" {
		return text
	}
	return "\x1b]8;;" + url + "\x1b\\" + text + "\x1b]8;;\x1b\\"
}

// sourceColors gives each source a stable colour so mixed digs scan easily.
var sourceColors = []string{sgrMagenta, sgrCyan, sgrYellow, sgrGreen, sgrBlue, sgrRed}

func sourceColor(name string) string {
	h := 0
	for _, r := range name {
		h = h*31 + int(r)
	}
	if h < 0 {
		h = -h
	}
	return sourceColors[h%len(sourceColors)]
}

// textWidth is the number of terminal columns s occupies: wide East Asian
// characters take two, combining marks none.
func textWidth(s string) int {
	n := 0
	for _, r := range s {
		n += runeWidth(r)
	}
	return n
}

func runeWidth(r rune) int {
	switch {
	case unicode.Is(unicode.Mn, r), r == '​':
		return 0
	case r < 0x1100:
		return 1
	}
	switch width.LookupRune(r).Kind() {
	case width.EastAsianWide, width.EastAsianFullwidth:
		return 2
	}
	return 1
}

// truncate cuts s to at most w columns, ending in "…" when cut.
func truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if textWidth(s) <= w {
		return s
	}
	var b strings.Builder
	used := 0
	for _, r := range s {
		rw := runeWidth(r)
		if used+rw > w-1 {
			break
		}
		b.WriteRune(r)
		used += rw
	}
	return strings.TrimRight(b.String(), " ") + "…"
}

// pad returns the spaces needed to fill plain text out to w columns.
func pad(plain string, w int) string {
	if n := w - textWidth(plain); n > 0 {
		return strings.Repeat(" ", n)
	}
	return ""
}

// oneLine collapses whitespace so table cells stay on one row.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
