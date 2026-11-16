// Package mail sends the panel's few emails (password resets, invites) over
// SMTP.
package mail

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

// Message is one email. Text is required; the panel sends plain text only,
// which every client shows and no client renders into a tracking pixel.
type Message struct {
	To      string
	Subject string
	Text    string
}

type Sender interface {
	Send(ctx context.Context, m Message) error
}

type Config struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string // "GamePanel <panel@example.com>" or an address
	// TLS: "starttls" (default, port 587), "tls" (implicit, port 465) or
	// "none" (only for a relay on the same machine or network).
	TLS string
}

type SMTP struct {
	cfg  Config
	from *mail.Address
}

func New(cfg Config) (*SMTP, error) {
	if cfg.Host == "" {
		return nil, errors.New("SMTP host is required")
	}
	from, err := mail.ParseAddress(cfg.From)
	if err != nil {
		return nil, fmt.Errorf("SMTP from address: %w", err)
	}
	switch cfg.TLS {
	case "":
		cfg.TLS = "starttls"
	case "starttls", "tls", "none":
	default:
		return nil, errors.New("SMTP TLS must be starttls, tls or none")
	}
	if cfg.Port == 0 {
		cfg.Port = map[string]int{"starttls": 587, "tls": 465, "none": 25}[cfg.TLS]
	}
	return &SMTP{cfg: cfg, from: from}, nil
}

// header keeps a header value on one line (no header injection through a
// subject or an address).
func header(s string) string { return strings.NewReplacer("\r", " ", "\n", " ").Replace(s) }

// Build renders m as an RFC 5322 message.
func (s *SMTP) Build(m Message) ([]byte, error) {
	to, err := mail.ParseAddress(m.To)
	if err != nil {
		return nil, fmt.Errorf("recipient: %w", err)
	}
	id := make([]byte, 12)
	rand.Read(id)
	domain := s.from.Address[strings.LastIndexByte(s.from.Address, '@')+1:]
	var b bytes.Buffer
	fmt.Fprintf(&b, "From: %s\r\n", s.from.String())
	fmt.Fprintf(&b, "To: %s\r\n", to.String())
	fmt.Fprintf(&b, "Subject: %s\r\n", mime.QEncoding.Encode("utf-8", header(m.Subject)))
	fmt.Fprintf(&b, "Date: %s\r\n", time.Now().Format(time.RFC1123Z))
	fmt.Fprintf(&b, "Message-ID: <%s@%s>\r\n", hex.EncodeToString(id), domain)
	b.WriteString("MIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: quoted-printable\r\n")
	b.WriteString("Auto-Submitted: auto-generated\r\n\r\n")
	qp := quotedprintable.NewWriter(&b)
	qp.Write([]byte(strings.ReplaceAll(m.Text, "\n", "\r\n")))
	qp.Close()
	return b.Bytes(), nil
}

func (s *SMTP) Send(ctx context.Context, m Message) error {
	msg, err := s.Build(m)
	if err != nil {
		return err
	}
	to, _ := mail.ParseAddress(m.To)
	addr := net.JoinHostPort(s.cfg.Host, strconv.Itoa(s.cfg.Port))
	d := net.Dialer{Timeout: 15 * time.Second}
	var conn net.Conn
	tlsCfg := &tls.Config{ServerName: s.cfg.Host, MinVersion: tls.VersionTLS12}
	if s.cfg.TLS == "tls" {
		conn, err = tls.DialWithDialer(&d, "tcp", addr, tlsCfg)
	} else {
		conn, err = d.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return err
	}
	if dl, ok := ctx.Deadline(); ok {
		conn.SetDeadline(dl)
	} else {
		conn.SetDeadline(time.Now().Add(time.Minute))
	}
	c, err := smtp.NewClient(conn, s.cfg.Host)
	if err != nil {
		conn.Close()
		return err
	}
	defer c.Close()
	if s.cfg.TLS == "starttls" {
		if err := c.StartTLS(tlsCfg); err != nil {
			return fmt.Errorf("starttls: %w", err)
		}
	}
	if s.cfg.Username != "" {
		// smtp.PlainAuth refuses to send the password without TLS (except to localhost).
		if err := c.Auth(smtp.PlainAuth("", s.cfg.Username, s.cfg.Password, s.cfg.Host)); err != nil {
			return fmt.Errorf("auth: %w", err)
		}
	}
	if err := c.Mail(s.from.Address); err != nil {
		return err
	}
	if err := c.Rcpt(to.Address); err != nil {
		return err
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}
