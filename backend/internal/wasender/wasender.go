// Package wasender delivers WhatsApp OTP messages.
// The log driver prints the code. Provider is a stub for Fonnte, Wablas,
// or the Meta WhatsApp Cloud API.
package wasender

import (
	"context"
	"fmt"
	"log/slog"
)

// Message is one outbound WhatsApp text.
type Message struct {
	To   string
	Body string
}

// Sender delivers one message.
type Sender interface {
	Send(ctx context.Context, msg Message) error
}

// Log prints the code to the process log.
type Log struct{}

func (Log) Send(_ context.Context, msg Message) error {
	slog.Info("dev whatsapp", "to", msg.To, "body", msg.Body)
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

// Provider is configured from WA_PROVIDER, WA_API_URL, WA_API_TOKEN, and WA_SENDER_ID.
// Sending is not implemented; the OTP service surfaces a generic failure.
type Provider struct {
	Name     string
	APIURL   string
	Token    string
	SenderID string
}

func (p Provider) Send(context.Context, Message) error {
	name := p.Name
	if name == "" {
		name = "whatsapp"
	}
	if p.APIURL == "" || p.Token == "" {
		return fmt.Errorf("whatsapp provider %s needs WA_API_URL and WA_API_TOKEN", name)
	}
	return fmt.Errorf("whatsapp provider %s is not implemented", name)
}
