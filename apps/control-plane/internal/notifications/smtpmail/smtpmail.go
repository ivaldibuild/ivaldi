// Package smtpmail sends notifications' emails over SMTP, implementing
// notifications.Mailer with the standard library's net/smtp.
package smtpmail

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
	"time"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/notifications"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/email"
	"github.com/ivaldibuild/ivaldi/pkg/id"
)

// Security is how the connection to the server is protected.
type Security string

const (
	// StartTLS connects in plain text and upgrades with STARTTLS, refusing a
	// server that does not offer it. It is the usual mode on port 587.
	StartTLS Security = "starttls"
	// TLS connects over TLS from the start, as on port 465.
	TLS Security = "tls"
	// None sends in plain text. It is only for a server on the same host,
	// such as a local relay.
	None Security = "none"
)

// timeout bounds one message's whole conversation with the server.
const timeout = 30 * time.Second

// Config says where and how to send.
type Config struct {
	// Address is the server's host:port.
	Address string
	// Username and Password authenticate with PLAIN, when Username is set.
	Username string
	Password string
	// From is the sender, such as "Ivaldi <ivaldi@example.com>".
	From     string
	Security Security
}

type mailer struct {
	host     string
	cfg      Config
	from     *mail.Address
	tlsCfg   *tls.Config
	hostname string
}

// New returns a Mailer that sends through the server in cfg, or an error
// when cfg is not usable.
func New(cfg Config) (notifications.Mailer, error) {
	host, _, err := net.SplitHostPort(cfg.Address)
	if err != nil {
		return nil, fmt.Errorf("smtp address %q must be host:port: %w", cfg.Address, err)
	}
	from, err := mail.ParseAddress(cfg.From)
	if err != nil {
		return nil, fmt.Errorf("smtp sender %q: %w", cfg.From, err)
	}
	switch cfg.Security {
	case StartTLS, TLS:
	case None:
		if !IsLoopback(host) {
			return nil, fmt.Errorf("smtp security none sends in plain text, so it is only allowed for a server on this host, not %s", host)
		}
	default:
		return nil, fmt.Errorf("smtp security %q must be starttls, tls, or none", cfg.Security)
	}
	return &mailer{
		host: host, cfg: cfg, from: from,
		tlsCfg:   &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12},
		hostname: domainOf(from.Address),
	}, nil
}

// IsLoopback reports whether host names this machine.
func IsLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (m *mailer) Send(ctx context.Context, msg notifications.Message) error {
	if !email.Valid(msg.To) {
		return fmt.Errorf("send email: %q is not an email address", msg.To)
	}
	body, err := m.format(msg)
	if err != nil {
		return err
	}
	c, err := m.connect(ctx)
	if err != nil {
		return err
	}
	defer c.Close()
	if m.cfg.Username != "" {
		if err := c.Auth(smtp.PlainAuth("", m.cfg.Username, m.cfg.Password, m.host)); err != nil {
			return fmt.Errorf("smtp authenticate: %w", err)
		}
	}
	if err := c.Mail(m.from.Address); err != nil {
		return fmt.Errorf("smtp sender: %w", err)
	}
	if err := c.Rcpt(msg.To); err != nil {
		return fmt.Errorf("smtp recipient: %w", err)
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("smtp data: %w", err)
	}
	if _, err := w.Write(body); err != nil {
		return fmt.Errorf("smtp write: %w", err)
	}
	// Closing the data writer is when the server accepts the message.
	if err := w.Close(); err != nil {
		return fmt.Errorf("smtp send: %w", err)
	}
	// The message is accepted; a failed goodbye does not undo that.
	_ = c.Quit()
	return nil
}

// connect opens a session with the server, protected as configured, with a
// deadline over the whole conversation.
func (m *mailer) connect(ctx context.Context) (*smtp.Client, error) {
	dialer := &net.Dialer{Timeout: timeout}
	conn, err := dialer.DialContext(ctx, "tcp", m.cfg.Address)
	if err != nil {
		return nil, fmt.Errorf("smtp connect: %w", err)
	}
	deadline := time.Now().Add(timeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err := conn.SetDeadline(deadline); err != nil {
		conn.Close()
		return nil, err
	}
	if m.cfg.Security == TLS {
		conn = tls.Client(conn, m.tlsCfg)
	}
	c, err := smtp.NewClient(conn, m.host)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("smtp greeting: %w", err)
	}
	if m.cfg.Security == StartTLS {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			c.Close()
			return nil, errors.New("smtp server does not offer STARTTLS")
		}
		if err := c.StartTLS(m.tlsCfg); err != nil {
			c.Close()
			return nil, fmt.Errorf("smtp starttls: %w", err)
		}
	}
	return c, nil
}

// format writes the message as plain text in UTF-8. The subject cannot
// carry a line break into the headers, and the body is quoted-printable,
// so no text from a notification can change the message's structure.
func (m *mailer) format(msg notifications.Message) ([]byte, error) {
	subject := strings.Join(strings.Fields(msg.Subject), " ")
	var b bytes.Buffer
	header := func(name, value string) { fmt.Fprintf(&b, "%s: %s\r\n", name, value) }
	header("From", m.from.String())
	header("To", msg.To)
	header("Subject", mime.QEncoding.Encode("utf-8", subject))
	header("Date", clock.Now().Format(time.RFC1123Z))
	header("Message-ID", fmt.Sprintf("<%s@%s>", id.ULID(), m.hostname))
	header("MIME-Version", "1.0")
	header("Content-Type", `text/plain; charset="utf-8"`)
	header("Content-Transfer-Encoding", "quoted-printable")
	b.WriteString("\r\n")
	w := quotedprintable.NewWriter(&b)
	lines := strings.ReplaceAll(strings.ReplaceAll(msg.Body, "\r\n", "\n"), "\n", "\r\n")
	if _, err := w.Write([]byte(lines)); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func domainOf(address string) string {
	if i := strings.LastIndex(address, "@"); i >= 0 {
		return address[i+1:]
	}
	return "localhost"
}
