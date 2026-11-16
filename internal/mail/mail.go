// Package mail builds and sends email.
//
// A Message is plain data; Build turns it into a standards-compliant MIME
// email (HTML with a plain-text alternative, plus attachments). A Sender
// delivers it: SMTP for real mail, File to write .eml/.html files for
// previews and tests. New senders (an HTTP mail API, say) only need to
// implement the one-method Sender interface.
package mail

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"net/textproto"
	"strings"
	"time"
)

// Message is one email to one or more recipients.
type Message struct {
	From        string // "Crate <music@example.com>"
	To          []string
	Subject     string
	Text        string // plain-text body
	HTML        string // HTML body (optional)
	Attachments []Attachment
	Date        time.Time
}

// Attachment is a file sent with the message.
type Attachment struct {
	Filename    string
	ContentType string
	Data        []byte
}

// Sender delivers messages.
type Sender interface {
	Send(ctx context.Context, m Message) error
}

// Validate checks addresses before anything is sent.
func (m Message) Validate() error {
	if _, err := mail.ParseAddress(m.From); err != nil {
		return fmt.Errorf("from address %q: %w", m.From, err)
	}
	if len(m.To) == 0 {
		return fmt.Errorf("no recipients")
	}
	for _, to := range m.To {
		if _, err := mail.ParseAddress(to); err != nil {
			return fmt.Errorf("recipient %q: %w", to, err)
		}
	}
	return nil
}

// Build renders the message as MIME bytes:
//
//	multipart/mixed            (only when there are attachments)
//	├── multipart/alternative
//	│   ├── text/plain
//	│   └── text/html
//	└── attachments…
func Build(m Message) ([]byte, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	if m.Date.IsZero() {
		m.Date = time.Now()
	}
	from, _ := mail.ParseAddress(m.From)

	var b bytes.Buffer
	h := func(k, v string) { fmt.Fprintf(&b, "%s: %s\r\n", k, v) }
	h("From", from.String())
	h("To", strings.Join(m.To, ", "))
	h("Subject", mime.QEncoding.Encode("utf-8", m.Subject))
	h("Date", m.Date.Format(time.RFC1123Z))
	h("Message-ID", messageID(from.Address))
	h("MIME-Version", "1.0")

	body, contentType, err := alternative(m)
	if err != nil {
		return nil, err
	}
	if len(m.Attachments) == 0 {
		h("Content-Type", contentType)
		b.WriteString("\r\n")
		b.Write(body)
		return b.Bytes(), nil
	}

	mixed := multipart.NewWriter(&b)
	h("Content-Type", `multipart/mixed; boundary="`+mixed.Boundary()+`"`)
	b.WriteString("\r\n")
	part, err := mixed.CreatePart(textproto.MIMEHeader{"Content-Type": {contentType}})
	if err != nil {
		return nil, err
	}
	part.Write(body)
	for _, a := range m.Attachments {
		ct := a.ContentType
		if ct == "" {
			ct = "application/octet-stream"
		}
		part, err := mixed.CreatePart(textproto.MIMEHeader{
			"Content-Type":              {ct},
			"Content-Transfer-Encoding": {"base64"},
			"Content-Disposition":       {mime.FormatMediaType("attachment", map[string]string{"filename": a.Filename})},
		})
		if err != nil {
			return nil, err
		}
		writeBase64Lines(part, a.Data)
	}
	if err := mixed.Close(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// alternative builds the text (+ HTML) body and returns its Content-Type.
func alternative(m Message) ([]byte, string, error) {
	if m.HTML == "" {
		var b bytes.Buffer
		writeQP(&b, m.Text)
		return b.Bytes(), "text/plain; charset=utf-8\r\nContent-Transfer-Encoding: quoted-printable", nil
	}
	var b bytes.Buffer
	alt := multipart.NewWriter(&b)
	for _, p := range []struct{ ct, body string }{
		{"text/plain; charset=utf-8", m.Text},
		{"text/html; charset=utf-8", m.HTML},
	} {
		w, err := alt.CreatePart(textproto.MIMEHeader{
			"Content-Type":              {p.ct},
			"Content-Transfer-Encoding": {"quoted-printable"},
		})
		if err != nil {
			return nil, "", err
		}
		writeQP(w, p.body)
	}
	if err := alt.Close(); err != nil {
		return nil, "", err
	}
	return b.Bytes(), `multipart/alternative; boundary="` + alt.Boundary() + `"`, nil
}

type writer interface{ Write([]byte) (int, error) }

func writeQP(w writer, s string) {
	q := quotedprintable.NewWriter(w)
	q.Write([]byte(strings.ReplaceAll(s, "\n", "\r\n")))
	q.Close()
}

// writeBase64Lines wraps base64 at 76 characters, as MIME requires.
func writeBase64Lines(w writer, data []byte) {
	enc := base64.StdEncoding.EncodeToString(data)
	for len(enc) > 76 {
		w.Write([]byte(enc[:76] + "\r\n"))
		enc = enc[76:]
	}
	w.Write([]byte(enc + "\r\n"))
}

func messageID(from string) string {
	domain := "cratecli.local"
	if _, d, ok := strings.Cut(from, "@"); ok && d != "" {
		domain = d
	}
	var r [12]byte
	rand.Read(r[:])
	return "<" + hex.EncodeToString(r[:]) + "." + "cratecli@" + domain + ">"
}
