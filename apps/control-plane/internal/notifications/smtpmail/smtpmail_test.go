package smtpmail

import (
	"bufio"
	"context"
	"io"
	"mime/quotedprintable"
	"net"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/notifications"
)

// fakeServer answers one SMTP conversation on loopback, offering AUTH PLAIN
// but not STARTTLS, and records what it was sent.
type fakeServer struct {
	addr     string
	commands chan []string
	data     chan string
}

func newFakeServer(t *testing.T) *fakeServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { ln.Close() })
	s := &fakeServer{addr: ln.Addr().String(), commands: make(chan []string, 1), data: make(chan string, 1)}
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		r := bufio.NewReader(conn)
		reply := func(line string) { io.WriteString(conn, line+"\r\n") }
		reply("220 fake ESMTP")
		var commands []string
		defer func() { s.commands <- commands }()
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimRight(line, "\r\n")
			commands = append(commands, line)
			switch verb := strings.ToUpper(strings.Fields(line + " x")[0]); verb {
			case "EHLO":
				reply("250-fake")
				reply("250 AUTH PLAIN")
			case "AUTH":
				reply("235 ok")
			case "MAIL", "RCPT":
				reply("250 ok")
			case "DATA":
				reply("354 go on")
				var data strings.Builder
				for {
					l, err := r.ReadString('\n')
					if err != nil || l == ".\r\n" {
						break
					}
					data.WriteString(l)
				}
				s.data <- data.String()
				reply("250 accepted")
			case "QUIT":
				reply("221 bye")
				return
			default:
				reply("500 unknown")
			}
		}
	}()
	return s
}

func TestSendsAMessage(t *testing.T) {
	server := newFakeServer(t)
	m, err := New(Config{Address: server.addr, Username: "ivaldi", Password: "secret", From: "Ivaldi <ivaldi@example.com>", Security: None})
	require.NoError(t, err)

	require.NoError(t, m.Send(context.Background(), notifications.Message{
		To:      "grace@example.com",
		Subject: "Approval needed: Deploy api to Production\r\nBcc: mallory@example.com",
		Body:    "Waiting for your approval: an agent asked\n\nhttps://ivaldi.example.com/approvals/apr_1",
	}))

	data := <-server.data
	headers, body, _ := strings.Cut(data, "\r\n\r\n")
	assert.Contains(t, headers, "To: grace@example.com\r\n")
	assert.Contains(t, headers, "Subject: Approval needed: Deploy api to Production Bcc: mallory@example.com\r\n",
		"a line break in the subject cannot start a header")
	assert.NotContains(t, headers, "\r\nBcc:")
	decoded, err := io.ReadAll(quotedprintable.NewReader(strings.NewReader(body)))
	require.NoError(t, err)
	assert.Contains(t, string(decoded), "https://ivaldi.example.com/approvals/apr_1")

	commands := <-server.commands
	assert.Contains(t, commands, "MAIL FROM:<ivaldi@example.com>")
	assert.Contains(t, commands, "RCPT TO:<grace@example.com>")
	assert.True(t, strings.HasPrefix(commands[1], "AUTH PLAIN "), "it authenticates when a username is set")
}

func TestRefusesToSendWithoutTheProtectionAskedFor(t *testing.T) {
	server := newFakeServer(t)
	m, err := New(Config{Address: server.addr, Username: "ivaldi", Password: "secret", From: "ivaldi@example.com", Security: StartTLS})
	require.NoError(t, err)
	err = m.Send(context.Background(), notifications.Message{To: "grace@example.com", Subject: "s", Body: "b"})
	assert.ErrorContains(t, err, "does not offer STARTTLS")
	for _, c := range <-server.commands {
		assert.False(t, strings.HasPrefix(c, "AUTH"), "no credentials go out in plain text")
	}

	_, err = New(Config{Address: "smtp.example.com:25", From: "ivaldi@example.com", Security: None})
	assert.ErrorContains(t, err, "only allowed for a server on this host")
}
