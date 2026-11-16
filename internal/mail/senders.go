package mail

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/mail"
	"net/smtp"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// SMTP sends through an SMTP server with STARTTLS, e.g. iCloud
// (smtp.mail.me.com:587), Gmail (smtp.gmail.com:587) or a local Mailpit
// (localhost:1025, no login).
type SMTP struct {
	Host     string
	Port     int
	Username string // empty = no login (local test servers)
	Password string // an app-specific password, never your account password
	Timeout  time.Duration
}

// Send delivers m to every recipient in one SMTP transaction. To keep
// recipients private from each other, call Send once per recipient.
func (s SMTP) Send(ctx context.Context, m Message) error {
	raw, err := Build(m)
	if err != nil {
		return err
	}
	if s.Port == 0 {
		s.Port = 587
	}
	if s.Timeout == 0 {
		s.Timeout = 30 * time.Second
	}
	addr := net.JoinHostPort(s.Host, strconv.Itoa(s.Port))
	ctx, cancel := context.WithTimeout(ctx, s.Timeout)
	defer cancel()

	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("smtp %s: %w", addr, err)
	}
	if dl, ok := ctx.Deadline(); ok {
		conn.SetDeadline(dl)
	}
	c, err := smtp.NewClient(conn, s.Host)
	if err != nil {
		conn.Close()
		return fmt.Errorf("smtp %s: %w", addr, err)
	}
	defer c.Close()

	if ok, _ := c.Extension("STARTTLS"); ok {
		if err := c.StartTLS(&tls.Config{ServerName: s.Host, MinVersion: tls.VersionTLS12}); err != nil {
			return fmt.Errorf("smtp starttls: %w", err)
		}
	} else if s.Username != "" && !isLocal(s.Host) {
		return fmt.Errorf("smtp %s doesn't offer STARTTLS; refusing to send a password unencrypted", addr)
	}
	if s.Username != "" {
		if err := c.Auth(smtp.PlainAuth("", s.Username, s.Password, s.Host)); err != nil {
			return fmt.Errorf("smtp login as %s: %w", s.Username, err)
		}
	}
	from, _ := mail.ParseAddress(m.From)
	if err := c.Mail(from.Address); err != nil {
		return fmt.Errorf("smtp from %s: %w", from.Address, err)
	}
	for _, to := range m.To {
		a, _ := mail.ParseAddress(to)
		if err := c.Rcpt(a.Address); err != nil {
			return fmt.Errorf("smtp recipient %s: %w", a.Address, err)
		}
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(raw); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("smtp: server rejected the message: %w", err)
	}
	return c.Quit()
}

func isLocal(host string) bool {
	return host == "localhost" || strings.HasPrefix(host, "127.") || host == "::1" || host == "mailpit"
}

// File writes each message to Dir as .eml (the full email) and .html (the
// HTML body, to open in a browser). Nothing is sent.
type File struct {
	Dir string
	// Written receives the paths of the files written.
	Written func(eml, html string)
}

// Send writes m to disk.
func (f File) Send(_ context.Context, m Message) error {
	raw, err := Build(m)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(f.Dir, 0o700); err != nil {
		return err
	}
	base := filepath.Join(f.Dir, fileStem(m))
	eml, html := base+".eml", base+".html"
	if err := os.WriteFile(eml, raw, 0o600); err != nil {
		return err
	}
	if m.HTML != "" {
		if err := os.WriteFile(html, []byte(m.HTML), 0o600); err != nil {
			return err
		}
	} else {
		html = ""
	}
	if f.Written != nil {
		f.Written(eml, html)
	}
	return nil
}

func fileStem(m Message) string {
	to := "nobody"
	if len(m.To) > 0 {
		if a, err := mail.ParseAddress(m.To[0]); err == nil {
			to = strings.NewReplacer("@", "_at_", ".", "_").Replace(a.Address)
		}
	}
	date := m.Date
	if date.IsZero() {
		date = time.Now()
	}
	subj := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		}
		return '-'
	}, m.Subject)
	if len(subj) > 40 {
		subj = subj[:40]
	}
	return date.Format("20060102-150405") + "-" + strings.Trim(subj, "-") + "-" + to
}
