// Package mailer sends transactional email. The log driver prints the link
// when SMTP is not configured.
package mailer

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
)

// Message is a plain-text email. Callers must not put a password in it.
type Message struct {
	To      string
	Subject string
	Text    string
}

// Sender delivers one message.
type Sender interface {
	Send(ctx context.Context, msg Message) error
}

// Log writes the message, including the link, to the process log.
type Log struct{}

func (Log) Send(_ context.Context, msg Message) error {
	slog.Info("dev mail", "to", msg.To, "subject", msg.Subject, "body", msg.Text)
	return nil
}

// Capture stores messages for tests.
type Capture struct {
	Messages []Message
	Err      error
}

func (c *Capture) Send(_ context.Context, msg Message) error {
	if c.Err != nil {
		return c.Err
	}
	c.Messages = append(c.Messages, msg)
	return nil
}

// SMTP talks to a relay. Port 465 uses implicit TLS. Other ports use STARTTLS when offered.
type SMTP struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
}

func (s SMTP) Send(ctx context.Context, msg Message) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.Host == "" || s.From == "" {
		return fmt.Errorf("smtp is not configured")
	}
	if strings.ContainsAny(msg.To, "\r\n") || strings.ContainsAny(msg.Subject, "\r\n") || strings.ContainsAny(s.From, "\r\n") {
		return fmt.Errorf("invalid mail header")
	}
	fromAddr, err := mail.ParseAddress(s.From)
	if err != nil {
		return fmt.Errorf("smtp from: %w", err)
	}
	body := strings.Join([]string{
		"From: " + s.From,
		"To: " + msg.To,
		"Subject: " + msg.Subject,
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"",
		msg.Text,
	}, "\r\n")
	addr := net.JoinHostPort(s.Host, strconv.Itoa(s.Port))
	var auth smtp.Auth
	if s.Username != "" {
		auth = smtp.PlainAuth("", s.Username, s.Password, s.Host)
	}
	if s.Port == 465 {
		return sendImplicitTLS(addr, s.Host, auth, fromAddr.Address, msg.To, []byte(body))
	}
	return smtp.SendMail(addr, auth, fromAddr.Address, []string{msg.To}, []byte(body))
}

func sendImplicitTLS(addr, host string, auth smtp.Auth, from, to string, body []byte) error {
	conn, err := tls.Dial("tcp", addr, &tls.Config{ServerName: host})
	if err != nil {
		return err
	}
	defer conn.Close()
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		return err
	}
	defer c.Close()
	if auth != nil {
		if err := c.Auth(auth); err != nil {
			return err
		}
	}
	if err := c.Mail(from); err != nil {
		return err
	}
	if err := c.Rcpt(to); err != nil {
		return err
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(body); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}
