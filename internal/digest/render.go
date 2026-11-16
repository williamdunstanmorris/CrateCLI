package digest

import (
	"bytes"
	"fmt"
	"html/template"
	"strings"
	textTemplate "text/template"

	"github.com/williamdunstanmorris/cratecli/internal/crate"
	"github.com/williamdunstanmorris/cratecli/internal/mail"
	"github.com/williamdunstanmorris/cratecli/internal/output"
)

// DefaultMaxPerSection keeps emails under Gmail's ~100 KB clipping limit.
const DefaultMaxPerSection = 40

// view is what the templates see: the issue, numbered and trimmed.
type view struct {
	Title    string
	Kicker   string // "CRATE · TUESDAY 17 NOV"
	Summary  string
	Sections []viewSection
	Footer   string
}

type viewSection struct {
	Title   string
	Count   int
	Rows    []viewRow
	More    int // records left out of the email (they're in the CSV)
	Problem string
}

type viewRow struct {
	N        int
	Artist   string
	Title    string
	URL      string // best link: the video, else a listen page, else a search
	Listen   string // "▶ Watch", "♪ Listen", "⌕ Search"
	Meta     string // label · catno · year · styles
	Note     string
	Price    string
	ShopURL  string
	ShopName string
	Discogs  string
}

// Render turns an issue into an email (without recipients). host says
// where it was sent from, for the footer.
func Render(is *Issue, from, host string) (mail.Message, error) {
	v, cut := buildView(is, host)
	var html, text bytes.Buffer
	if err := htmlTmpl.Execute(&html, v); err != nil {
		return mail.Message{}, err
	}
	if err := textTmpl.Execute(&text, v); err != nil {
		return mail.Message{}, err
	}
	m := mail.Message{
		From:    from,
		Subject: Subject(is),
		HTML:    html.String(),
		Text:    text.String(),
		Date:    is.Date,
	}
	if cut {
		var csv bytes.Buffer
		if err := output.Write(&csv, "csv", is.Records()); err != nil {
			return mail.Message{}, err
		}
		m.Attachments = []mail.Attachment{{
			Filename:    fmt.Sprintf("crate-%s-%s.csv", is.Digest.Name, is.Date.Format("2006-01-02")),
			ContentType: "text/csv; charset=utf-8",
			Data:        csv.Bytes(),
		}}
	}
	return m, nil
}

// Subject is "Crate · Tuesday · Berlin shops: 34 new".
func Subject(is *Issue) string {
	n := len(is.Records())
	s := fmt.Sprintf("Crate · %s · %s", is.Date.Format("Monday"), is.Digest.DisplayTitle())
	switch n {
	case 0:
		return s + ": nothing new"
	case 1:
		return s + ": 1 new record"
	}
	return fmt.Sprintf("%s: %d new records", s, n)
}

func buildView(is *Issue, host string) (view, bool) {
	max := is.Digest.MaxPerSection
	if max <= 0 {
		max = DefaultMaxPerSection
	}
	v := view{
		Title:  is.Digest.DisplayTitle(),
		Kicker: strings.ToUpper("Crate · " + is.Date.Format("Monday 2 Jan")),
	}
	total, n, cut := 0, 0, false
	var names []string
	for _, s := range is.Sections {
		vs := viewSection{Title: s.Title, Count: len(s.Records)}
		if s.Err != nil {
			vs.Problem = "Couldn't fetch this section: " + s.Err.Error()
		}
		for i, r := range s.Records {
			n++ // numbering continues across sections and includes cut rows
			if i >= max {
				vs.More++
				cut = true
				continue
			}
			vs.Rows = append(vs.Rows, row(n, r))
		}
		total += len(s.Records)
		if len(s.Records) > 0 {
			names = append(names, s.Title)
		}
		v.Sections = append(v.Sections, vs)
	}
	switch total {
	case 0:
		v.Summary = "Nothing new since the last one."
	case 1:
		v.Summary = "1 new record."
	default:
		v.Summary = fmt.Sprintf("%d new records from %s.", total, joinAnd(names))
	}
	v.Footer = "Sent by cratecli"
	if host != "" {
		v.Footer += " on " + host
	}
	v.Footer += " at " + is.Date.Format("15:04") + ". Records shown here won't appear in your digs again."
	return v, cut
}

func row(n int, r crate.Record) viewRow {
	vr := viewRow{
		N:       n,
		Artist:  crate.CleanArtist(r.Artist),
		Title:   r.Title,
		URL:     r.BestURL(),
		Note:    r.Note,
		Price:   output.TidyPrice(r.Price),
		ShopURL: r.SourceURL,
		Discogs: r.DiscogsURL,
	}
	switch {
	case len(r.Videos) > 0:
		vr.Listen = "▶ Watch"
	case r.ListenURL != "":
		vr.Listen = "♪ Listen"
	default:
		vr.Listen = "⌕ Search YouTube"
	}
	if name, _, _ := strings.Cut(r.Source, ":"); name != "" && r.SourceURL != "" {
		vr.ShopName = name
	}
	tags := r.Styles
	if len(tags) == 0 {
		// The source's own genre tags, minus its section name.
		_, section, _ := strings.Cut(r.Source, ":")
		for _, t := range r.Tags {
			if !strings.EqualFold(t, section) {
				tags = append(tags, t)
			}
		}
	}
	var meta []string
	for _, p := range []string{r.Label, r.CatNo, r.Year, strings.Join(firstN(tags, 3), ", ")} {
		if p = strings.TrimSpace(p); p != "" && p != "0" {
			meta = append(meta, p)
		}
	}
	vr.Meta = strings.Join(meta, " · ")
	if len([]rune(vr.Note)) > 220 {
		vr.Note = string([]rune(vr.Note)[:219]) + "…"
	}
	return vr
}

func firstN(s []string, n int) []string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

func joinAnd(names []string) string {
	switch len(names) {
	case 0:
		return "your sources"
	case 1:
		return names[0]
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}

// The HTML uses tables and inline styles because email apps ignore most
// CSS. The <style> block only adds dark-mode colours for apps that
// support it (Apple Mail); others use the inline light colours.
var htmlTmpl = template.Must(template.New("html").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="color-scheme" content="light dark">
<meta name="supported-color-schemes" content="light dark">
<title>{{.Title}}</title>
<style>
@media (prefers-color-scheme: dark) {
  .bg { background:#16161a !important; }
  .card { background:#1f1f24 !important; }
  .ink { color:#ececf0 !important; }
  .muted { color:#a0a0aa !important; }
  .rule { border-color:#33333a !important; }
  .chip { background:#2c2c33 !important; color:#ececf0 !important; }
}
</style>
</head>
<body class="bg" style="margin:0;padding:0;background:#f4f3ef;">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" class="bg" style="background:#f4f3ef;">
<tr><td align="center" style="padding:24px 12px;">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" class="card" style="max-width:640px;background:#ffffff;border-radius:10px;">
<tr><td style="padding:28px 28px 8px 28px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Helvetica,Arial,sans-serif;">
  <div class="muted" style="font-size:11px;letter-spacing:1.5px;color:#8a8a8a;">{{.Kicker}}</div>
  <div class="ink" style="font-size:26px;font-weight:700;color:#18181b;margin-top:4px;">{{.Title}}</div>
  <div class="muted" style="font-size:14px;color:#6b6b70;margin-top:6px;">{{.Summary}}</div>
</td></tr>
{{range .Sections}}
<tr><td style="padding:22px 28px 0 28px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Helvetica,Arial,sans-serif;">
  <div class="ink" style="font-size:13px;font-weight:700;letter-spacing:1px;text-transform:uppercase;color:#18181b;">{{.Title}} <span class="muted" style="font-weight:400;color:#8a8a8a;">· {{.Count}}</span></div>
  {{if .Problem}}<div class="muted" style="font-size:13px;color:#b45309;margin-top:8px;">{{.Problem}}</div>{{end}}
  <table role="presentation" width="100%" cellpadding="0" cellspacing="0">
  {{range .Rows}}
  <tr><td class="rule" style="padding:14px 0;border-top:1px solid #ececec;">
    <div class="ink" style="font-size:15px;line-height:21px;color:#18181b;">
      <span class="muted" style="color:#a1a1aa;font-size:12px;">{{.N}}</span>&nbsp;
      <b>{{.Artist}}</b> – <a href="{{.URL}}" style="color:#c2410c;text-decoration:none;">{{.Title}}</a>
    </div>
    {{if .Meta}}<div class="muted" style="font-size:13px;line-height:19px;color:#6b6b70;margin-top:2px;">{{.Meta}}{{if .Price}} · {{.Price}}{{end}}</div>{{end}}
    {{if .Note}}<div class="muted" style="font-size:13px;line-height:19px;color:#6b6b70;font-style:italic;margin-top:4px;">“{{.Note}}”</div>{{end}}
    <div style="margin-top:8px;font-size:12px;">
      <a href="{{.URL}}" class="chip" style="display:inline-block;padding:4px 10px;border-radius:999px;background:#fbe9e0;color:#9a3412;text-decoration:none;">{{.Listen}}</a>
      {{if .ShopURL}}&nbsp;<a href="{{.ShopURL}}" class="muted" style="color:#6b6b70;">{{if .ShopName}}{{.ShopName}}{{else}}source{{end}}</a>{{end}}
      {{if .Discogs}}&nbsp;<a href="{{.Discogs}}" class="muted" style="color:#6b6b70;">discogs</a>{{end}}
    </div>
  </td></tr>
  {{end}}
  </table>
  {{if .More}}<div class="muted" style="font-size:13px;color:#6b6b70;padding:6px 0 0 0;">+ {{.More}} more in the attached CSV</div>{{end}}
</td></tr>
{{end}}
<tr><td style="padding:24px 28px 28px 28px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Helvetica,Arial,sans-serif;">
  <div class="muted rule" style="font-size:12px;line-height:18px;color:#8a8a8a;border-top:1px solid #ececec;padding-top:14px;">{{.Footer}}</div>
</td></tr>
</table>
</td></tr>
</table>
</body>
</html>
`))

var textTmpl = textTemplate.Must(textTemplate.New("text").Parse(`{{.Kicker}}
{{.Title}}
{{.Summary}}
{{range .Sections}}
== {{.Title}} ({{.Count}}) ==
{{if .Problem}}{{.Problem}}
{{end}}{{range .Rows}}
{{.N}}. {{.Artist}} – {{.Title}}
{{if .Meta}}   {{.Meta}}{{if .Price}} · {{.Price}}{{end}}
{{end}}{{if .Note}}   "{{.Note}}"
{{end}}   {{.URL}}
{{end}}{{if .More}}
+ {{.More}} more in the attached CSV
{{end}}{{end}}
--
{{.Footer}}
`))
