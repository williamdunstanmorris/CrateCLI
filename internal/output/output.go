// Package output renders records in different formats.
package output

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/williamdunstanmorris/cratecli/internal/crate"
)

// Formats lists the supported output formats.
var Formats = []string{"table", "long", "json", "jsonl", "csv", "urls", "m3u", "md"}

// Write renders recs to w in the named format as plain text.
func Write(w io.Writer, format string, recs []crate.Record) error {
	return WriteOpts(w, format, recs, Options{})
}

// WriteOpts is Write with terminal options for the table and long formats.
func WriteOpts(w io.Writer, format string, recs []crate.Record, o Options) error {
	switch strings.ToLower(format) {
	case "json":
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false)
		return enc.Encode(recs)
	case "jsonl", "ndjson":
		enc := json.NewEncoder(w)
		enc.SetEscapeHTML(false)
		for _, r := range recs {
			if err := enc.Encode(r); err != nil {
				return err
			}
		}
		return nil
	case "csv":
		return writeCSV(w, recs)
	case "urls":
		for _, r := range recs {
			fmt.Fprintln(w, r.BestURL())
		}
		return nil
	case "m3u":
		return writeM3U(w, recs)
	case "md", "markdown":
		return writeMarkdown(w, recs)
	case "table", "":
		return writeTable(w, recs, o)
	case "long":
		return writeLong(w, recs, o)
	}
	return fmt.Errorf("unknown format %q (want one of %s)", format, strings.Join(Formats, ", "))
}

// writeLong prints every detail of each record over several lines.
func writeLong(w io.Writer, recs []crate.Record, o Options) error {
	s := style{o}
	url := func(u string) string { return s.link(u, s.paint(sgrDim, u)) }
	for i, r := range recs {
		fmt.Fprintf(w, "%s %s %s %s\n", s.paint(sgrDim, fmt.Sprintf("%2d.", i+1)),
			s.paint(sgrBold, r.Artist), s.paint(sgrDim, "–"), r.Title)

		// Each line is a (plain label, colour, value) so labels can be
		// padded by visible width; colour codes would confuse tabwriter.
		type line struct{ label, color, value string }
		var lines []line
		add := func(label, color, value string) { lines = append(lines, line{label, color, value}) }
		if meta := compact(r.Label, r.CatNo, r.Year, r.Country, r.Format); meta != "" {
			add("", "", s.paint(sgrCyan, meta))
		}
		if len(r.Styles) > 0 {
			add("", "", s.paint(sgrDim, strings.Join(r.Styles, ", ")))
		} else if len(r.Tags) > 0 {
			add("", "", s.paint(sgrDim, strings.Join(r.Tags, ", ")))
		}
		if r.Note != "" {
			add("", "", s.paint(sgrItalic, "“"+r.Note+"”"))
		}
		if r.MyNote != "" {
			add("", "", s.paint(sgrMagenta, "✎ "+r.MyNote))
		}
		if r.Price != "" {
			p := r.Price
			if r.Condition != "" {
				p += "  (" + r.Condition + ")"
			}
			add("", "", s.paint(sgrYellow, p))
		}
		if r.Kind != "" && r.Kind != "database" {
			name := sourceName(r.Source)
			add(name, sourceColor(name), url(r.SourceURL))
		}
		if r.DiscogsURL != "" {
			add("discogs", sgrDim, url(r.DiscogsURL))
		}
		for _, v := range r.Videos {
			add("▶ "+v.Source, sgrGreen, s.link(v.URL, v.URL)+"  "+s.paint(sgrDim, v.Title))
		}
		if r.ListenURL != "" && r.ListenURL != r.SourceURL {
			add("listen", sgrBlue, url(r.ListenURL))
		}
		if len(r.Audio) > 0 {
			add("♪ "+r.Audio[0].Source, sgrBlue, url(r.Audio[0].URL))
		}
		if len(r.Videos) == 0 {
			add("search", sgrDim, url(r.YouTubeSearchURL))
		}
		lw := 0
		for _, l := range lines {
			lw = max(lw, textWidth(l.label))
		}
		for _, l := range lines {
			fmt.Fprintf(w, "    %s%s  %s\n", s.paint(l.color, l.label), pad(l.label, lw), l.value)
		}
		fmt.Fprintln(w)
	}
	return nil
}

// sourceName turns "hardwax:this-week" into "hardwax".
func sourceName(s string) string {
	name, _, _ := strings.Cut(s, ":")
	return name
}

func compact(parts ...string) string {
	var keep []string
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" && p != "0" {
			keep = append(keep, p)
		}
	}
	return strings.Join(keep, " · ")
}

func writeCSV(w io.Writer, recs []crate.Record) error {
	cw := csv.NewWriter(w)
	header := []string{"release_id", "artist", "title", "label", "catno", "year", "country",
		"format", "genres", "styles", "tags", "want", "have", "price", "condition", "source", "kind",
		"note", "source_url", "discogs_url", "listing_url", "video_1", "video_2", "video_3",
		"listen_url", "audio_url", "youtube_search_url", "my_note", "saved_at"}
	if err := cw.Write(header); err != nil {
		return err
	}
	for _, r := range recs {
		vids := make([]string, 3)
		for i := 0; i < len(r.Videos) && i < 3; i++ {
			vids[i] = r.Videos[i].URL
		}
		audio := ""
		if len(r.Audio) > 0 {
			audio = r.Audio[0].URL
		}
		id := ""
		if r.ReleaseID > 0 {
			id = strconv.Itoa(r.ReleaseID)
		}
		row := []string{id, r.Artist, r.Title, r.Label, r.CatNo, r.Year,
			r.Country, r.Format, strings.Join(r.Genres, "; "), strings.Join(r.Styles, "; "),
			strings.Join(r.Tags, "; "), strconv.Itoa(r.Want), strconv.Itoa(r.Have), r.Price,
			r.Condition, r.Source, r.Kind, r.Note, r.SourceURL, r.DiscogsURL, r.ListingURL,
			vids[0], vids[1], vids[2], r.ListenURL, audio, r.YouTubeSearchURL, r.MyNote, r.SavedAt}
		if err := cw.Write(row); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

// writeM3U produces a playlist that mpv/VLC can play directly (mpv uses
// yt-dlp to stream YouTube links): cratecli seller decks.de -f m3u > dig.m3u
func writeM3U(w io.Writer, recs []crate.Record) error {
	fmt.Fprintln(w, "#EXTM3U")
	for _, r := range recs {
		links := r.Videos
		if len(links) == 0 {
			// No video: fall back to the source's own audio (Bandcamp
			// stream, shop snippet).
			links = r.Audio
		}
		for _, v := range links {
			title := v.Title
			if title == "" {
				title = r.Query()
			}
			fmt.Fprintf(w, "#EXTINF:-1,%s\n%s\n", title, v.URL)
		}
	}
	return nil
}

func writeMarkdown(w io.Writer, recs []crate.Record) error {
	for _, r := range recs {
		link := orDefault(r.DiscogsURL, orDefault(r.SourceURL, r.YouTubeSearchURL))
		fmt.Fprintf(w, "### [%s - %s](%s)\n", r.Artist, r.Title, link)
		if meta := compact(r.Label, r.CatNo, r.Year, strings.Join(r.Styles, ", ")); meta != "" {
			fmt.Fprintf(w, "%s\n\n", meta)
		}
		switch {
		case r.Note != "" && r.SourceURL != "":
			fmt.Fprintf(w, "> %s ([%s](%s))\n\n", r.Note, sourceName(r.Source), r.SourceURL)
		case r.Note != "":
			fmt.Fprintf(w, "> %s\n\n", r.Note)
		}
		for _, v := range r.Videos {
			fmt.Fprintf(w, "- [%s](%s)\n", orDefault(v.Title, v.URL), v.URL)
		}
		if len(r.Videos) == 0 {
			fmt.Fprintf(w, "- [Search YouTube](%s)\n", r.YouTubeSearchURL)
		}
		fmt.Fprintln(w)
	}
	return nil
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}
