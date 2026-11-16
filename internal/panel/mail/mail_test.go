package mail

import (
	"bufio"
	"context"
	"net"
	"strconv"
	"strings"
	"testing"
)

func TestBuildKeepsHeadersOnOneLine(t *testing.T) {
	s, err := New(Config{Host: "smtp.example.com", From: "GamePanel <panel@example.com>"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.Build(Message{To: "bob@example.com", Subject: "Hi\r\nBcc: victim@example.com", Text: "line one\nline two"})
	if err != nil {
		t.Fatal(err)
	}
	head, body, _ := strings.Cut(string(b), "\r\n\r\n")
	if strings.Contains(head, "\r\nBcc:") {
		t.Fatalf("header injection:\n%s", head)
	}
	if !strings.Contains(body, "line one\r\nline two") {
		t.Fatalf("body %q", body)
	}
	if _, err := s.Build(Message{To: "not an address", Subject: "x", Text: "x"}); err == nil {
		t.Fatal("bad recipient accepted")
	}
}

// A tiny SMTP server that accepts one message.
func TestSendOverPlainSMTP(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	defer ln.Close()
	got := make(chan string, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		r := bufio.NewReader(c)
		w := func(s string) { c.Write([]byte(s + "\r\n")) }
		w("220 test")
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
					w("250 ok")
					got <- data.String()
					continue
				}
				data.WriteString(line)
				continue
			}
			switch cmd := strings.ToUpper(strings.TrimSpace(line)); {
			case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
				w("250 test")
			case strings.HasPrefix(cmd, "DATA"):
				w("354 go")
				inData = true
			case strings.HasPrefix(cmd, "QUIT"):
				w("221 bye")
				return
			default:
				w("250 ok")
			}
		}
	}()
	port, _ := strconv.Atoi(ln.Addr().(*net.TCPAddr).AddrPort().String()[strings.LastIndex(ln.Addr().String(), ":")+1:])
	s, _ := New(Config{Host: "127.0.0.1", Port: port, TLS: "none", From: "panel@example.com"})
	if err := s.Send(context.Background(), Message{To: "bob@example.com", Subject: "Reset", Text: "https://panel.example.com/reset#abc"}); err != nil {
		t.Fatal(err)
	}
	if m := <-got; !strings.Contains(m, "Subject: Reset") || !strings.Contains(m, "panel.example.com/reset") {
		t.Fatalf("message %q", m)
	}
}
