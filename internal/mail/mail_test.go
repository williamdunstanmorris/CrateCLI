package mail

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/mail"
	"strconv"
	"strings"
	"testing"
	"time"
)

func testMessage() Message {
	return Message{
		From:    "Crate <music@example.com>",
		To:      []string{"me@example.com"},
		Subject: "Crate · Tuesday · Berlin shops",
		Text:    "1. Chez Damier – Can You Feel It\nhttps://youtube.com/…",
		HTML:    `<p><a href="https://youtube.com/">Chez Damier – Can You Feel It</a></p>`,
		Date:    time.Date(2026, 11, 17, 7, 0, 0, 0, time.UTC),
	}
}

// TestBuildParses checks the output is a valid email a mail client can
// read back: headers decode, both bodies are there, attachment intact.
func TestBuildParses(t *testing.T) {
	m := testMessage()
	m.Attachments = []Attachment{{Filename: "crate.csv", ContentType: "text/csv", Data: []byte("artist,title\nA,B\n")}}
	raw, err := Build(m)
	if err != nil {
		t.Fatal(err)
	}
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	subj, _ := new(mime.WordDecoder).DecodeHeader(msg.Header.Get("Subject"))
	if subj != m.Subject {
		t.Errorf("subject = %q", subj)
	}
	mt, params, _ := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if mt != "multipart/mixed" {
		t.Fatalf("content type = %s", mt)
	}
	mr := multipart.NewReader(msg.Body, params["boundary"])
	var sawText, sawHTML, sawCSV bool
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		ct, ps, _ := mime.ParseMediaType(p.Header.Get("Content-Type"))
		switch {
		case ct == "multipart/alternative":
			ar := multipart.NewReader(p, ps["boundary"])
			for {
				ap, err := ar.NextPart() // decodes quoted-printable for us
				if err == io.EOF {
					break
				}
				body, _ := io.ReadAll(ap)
				if strings.HasPrefix(ap.Header.Get("Content-Type"), "text/plain") && strings.Contains(string(body), "Chez Damier – Can You Feel It") {
					sawText = true
				}
				if strings.HasPrefix(ap.Header.Get("Content-Type"), "text/html") && strings.Contains(string(body), `<a href="https://youtube.com/">`) {
					sawHTML = true
				}
			}
		case p.FileName() == "crate.csv":
			sawCSV = true
		}
	}
	if !sawText || !sawHTML || !sawCSV {
		t.Errorf("text %v html %v csv %v", sawText, sawHTML, sawCSV)
	}
}

func TestValidate(t *testing.T) {
	m := testMessage()
	m.To = []string{"not an address"}
	if _, err := Build(m); err == nil {
		t.Error("want error for bad recipient")
	}
}

// TestSMTPSend talks to a tiny fake SMTP server (no TLS, no login), the
// way Mailpit behaves locally.
func TestSMTPSend(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	got := make(chan string, 1)
	go fakeSMTP(t, ln, got)

	host, port, _ := net.SplitHostPort(ln.Addr().String())
	p, _ := strconv.Atoi(port)
	if err := (SMTP{Host: host, Port: p}).Send(context.Background(), testMessage()); err != nil {
		t.Fatal(err)
	}
	data := <-got
	if !strings.Contains(data, "Subject: =?utf-8?q?Crate_=C2=B7_Tuesday") {
		t.Errorf("server got:\n%s", data)
	}
}

func fakeSMTP(t *testing.T, ln net.Listener, got chan<- string) {
	conn, err := ln.Accept()
	if err != nil {
		return
	}
	defer conn.Close()
	r := bufio.NewReader(conn)
	say := func(s string) { conn.Write([]byte(s + "\r\n")) }
	say("220 fake")
	var data strings.Builder
	inData := false
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		if inData {
			if line == ".\r\n" {
				inData = false
				got <- data.String()
				say("250 ok")
				continue
			}
			data.WriteString(line)
			continue
		}
		switch cmd := strings.ToUpper(strings.TrimSpace(line)); {
		case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
			say("250 fake")
		case cmd == "DATA":
			inData = true
			say("354 go")
		case cmd == "QUIT":
			say("221 bye")
			return
		default:
			say("250 ok")
		}
	}
}
