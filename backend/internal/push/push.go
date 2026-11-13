package push

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"

	webpush "github.com/SherClockHolmes/webpush-go"
)

// Sender delivers Web Push messages. A missing key pair disables sending;
// in-app notifications still exist.
type Sender struct {
	Public  string
	Private string
	Subject string
	enabled bool
}

func Load(public, private, subject, dir string) (*Sender, error) {
	s := &Sender{Public: public, Private: private, Subject: subject}
	if s.Public == "" || s.Private == "" {
		path := filepath.Join(dir, "vapid.json")
		if b, err := os.ReadFile(path); err == nil {
			var saved struct {
				Public  string `json:"public"`
				Private string `json:"private"`
			}
			if json.Unmarshal(b, &saved) == nil && saved.Public != "" {
				s.Public, s.Private = saved.Public, saved.Private
			}
		}
	}
	if s.Public == "" || s.Private == "" {
		// A generated key pair on Vercel would change every cold start and
		// break subscriptions. Require the env pair there; local dev still
		// writes vapid.json into the upload directory.
		if os.Getenv("VERCEL") == "1" {
			return s, nil
		}
		priv, pub, err := webpush.GenerateVAPIDKeys()
		if err != nil {
			return s, err
		}
		s.Private, s.Public = priv, pub
		if dir != "" {
			_ = os.MkdirAll(dir, 0o755)
			b, _ := json.MarshalIndent(map[string]string{"public": pub, "private": priv}, "", "  ")
			_ = os.WriteFile(filepath.Join(dir, "vapid.json"), b, 0o600)
		}
	}
	s.enabled = s.Public != "" && s.Private != ""
	return s, nil
}

func (s *Sender) Enabled() bool {
	return s != nil && s.enabled
}

func (s *Sender) PublicKey() string {
	if s == nil {
		return ""
	}
	return s.Public
}

// Send returns the HTTP status from the push service. 404 and 410 mean the
// subscription is dead and the caller should delete it.
func (s *Sender) Send(endpoint, p256dh, auth string, payload []byte) (int, error) {
	if s == nil || !s.enabled {
		return 0, nil
	}
	resp, err := webpush.SendNotification(payload, &webpush.Subscription{
		Endpoint: endpoint,
		Keys:     webpush.Keys{P256dh: p256dh, Auth: auth},
	}, &webpush.Options{
		Subscriber:      s.Subject,
		VAPIDPublicKey:  s.Public,
		VAPIDPrivateKey: s.Private,
		TTL:             60,
	})
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone {
		return resp.StatusCode, nil
	}
	if resp.StatusCode >= 300 {
		return resp.StatusCode, nil
	}
	return resp.StatusCode, nil
}
