package service

import (
	"context"
	"errors"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"pesenin.product.biz.id/api/internal/db"
)

const (
	tokenTTL = 30 * time.Minute
	otpTTL   = 5 * time.Minute

	purposeVerify = "verify_email"
	purposeReset  = "reset_password"
	purposeLink   = "link_password"

	msgSignup = "Kalau emailnya bisa dipakai, Pingping kirim tautan. Cek kotak masuk ya."
	msgReset  = "Kalau email ini terdaftar di Pesenin, kami kirim tautan untuk atur ulang kata sandi. Cek kotak masuk ya."
	msgResend = "Kalau email ini menunggu konfirmasi, kami kirim tautan baru. Cek kotak masuk ya."
)

type authQuota struct {
	loginIP, loginEmail   int
	loginWindow           time.Duration
	signupIP, signupEmail int
	signupWindow          time.Duration
	resetIP, resetEmail   int
	resetWindow           time.Duration
	failMax               int
	failLock              time.Duration
	otpPerHour            int
	otpCooldown           time.Duration
}

func defaultQuota() authQuota {
	return authQuota{
		loginIP: 20, loginEmail: 10, loginWindow: 15 * time.Minute,
		signupIP: 8, signupEmail: 5, signupWindow: time.Hour,
		resetIP: 8, resetEmail: 3, resetWindow: time.Hour,
		failMax: 5, failLock: 10 * time.Minute,
		otpPerHour: 5, otpCooldown: time.Minute,
	}
}

func (a *App) quota() authQuota {
	if a.limits != nil {
		return *a.limits
	}
	return defaultQuota()
}

type userSecret struct {
	PasswordHash       *string
	EmailVerifiedAt    *time.Time
	Whatsapp           *string
	WhatsappVerifiedAt *time.Time
}

func (s userSecret) emailVerified() bool { return s.EmailVerifiedAt != nil }
func (s userSecret) waVerified() bool    { return s.WhatsappVerifiedAt != nil }
func (s userSecret) ready() bool         { return s.emailVerified() && s.waVerified() }
func (s userSecret) hasPassword() bool {
	return s.PasswordHash != nil && *s.PasswordHash != ""
}

func (a *App) loadSecret(ctx context.Context, id uuid.UUID) (userSecret, error) {
	var s userSecret
	err := a.Pool.QueryRow(ctx, `
		SELECT password_hash, email_verified_at, whatsapp_e164, whatsapp_verified_at
		FROM users WHERE id = $1`, id).Scan(&s.PasswordHash, &s.EmailVerifiedAt, &s.Whatsapp, &s.WhatsappVerifiedAt)
	return s, err
}

func (a *App) markEmailVerified(ctx context.Context, id uuid.UUID) error {
	_, err := a.Pool.Exec(ctx, `
		UPDATE users SET email_verified_at = COALESCE(email_verified_at, now()) WHERE id = $1`, id)
	return err
}

func normalizeEmail(in string) (string, error) {
	s := strings.ToLower(strings.TrimSpace(in))
	if s == "" || len(s) > 200 || strings.ContainsAny(s, " \t\r\n") {
		return "", E(400, "bad_email", "Email belum pas.")
	}
	addr, err := mail.ParseAddress(s)
	if err != nil || addr.Address != s {
		return "", E(400, "bad_email", "Email belum pas.")
	}
	parts := strings.Split(s, "@")
	if len(parts) != 2 || parts[0] == "" || !strings.Contains(parts[1], ".") {
		return "", E(400, "bad_email", "Email belum pas.")
	}
	return s, nil
}

func cleanName(in, email string) (string, error) {
	name := strings.TrimSpace(in)
	if utf8.RuneCountInString(name) > 80 {
		return "", E(400, "bad_name", "Nama maksimal 80 karakter.")
	}
	if name == "" {
		name = strings.Split(email, "@")[0]
	}
	return name, nil
}

func (a *App) allow(ipKey, emailKey string, ipLimit, emailLimit int, window time.Duration) error {
	if a.Limit == nil {
		return nil
	}
	if !a.Limit.Allow(ipKey, ipLimit, window) || !a.Limit.Allow(emailKey, emailLimit, window) {
		return E(429, "rate_limited", "Terlalu banyak permintaan. Coba lagi sebentar ya.")
	}
	return nil
}

func (a *App) loginLocked(email, ip string) bool {
	if a.Limit == nil {
		return false
	}
	return a.Limit.Locked("fail:login:email:"+email) || a.Limit.Locked("fail:login:ip:"+ip)
}

func (a *App) noteLoginFail(email, ip string) {
	if a.Limit == nil {
		return
	}
	q := a.quota()
	a.Limit.Fail("fail:login:email:"+email, q.failMax, q.failLock)
	a.Limit.Fail("fail:login:ip:"+ip, q.failMax, q.failLock)
}

func (a *App) clearLoginFail(email, ip string) {
	if a.Limit == nil {
		return
	}
	a.Limit.Clear("fail:login:email:" + email)
	a.Limit.Clear("fail:login:ip:" + ip)
}

// AdminSessionAllowed keeps /api/v1/admin on Google (and the local demo login).
// A password session is not enough, even when the user is a platform admin.
func AdminSessionAllowed(u db.User, via string) error {
	if !u.IsPlatformAdmin {
		return E(403, "forbidden", "Khusus admin Pesenin.")
	}
	switch via {
	case "", "google", "dev":
		return nil
	default:
		return E(403, "forbidden", "Admin Pesenin hanya lewat Google.")
	}
}

func (a *App) issueToken(ctx context.Context, userID uuid.UUID, purpose string) (string, error) {
	purposes := []string{purpose}
	if purpose == purposeReset || purpose == purposeLink {
		purposes = []string{purposeReset, purposeLink}
	}
	if _, err := a.Pool.Exec(ctx, `
		UPDATE auth_tokens SET used_at = now()
		WHERE user_id = $1 AND purpose = ANY($2) AND used_at IS NULL`, userID, purposes); err != nil {
		return "", err
	}
	raw, err := sessionToken()
	if err != nil {
		return "", err
	}
	_, err = a.Pool.Exec(ctx, `
		INSERT INTO auth_tokens (user_id, purpose, token_hash, expires_at)
		VALUES ($1, $2, $3, $4)`, userID, purpose, hashToken(raw), time.Now().Add(tokenTTL))
	if err != nil {
		return "", err
	}
	return raw, nil
}

func (a *App) consumeToken(ctx context.Context, raw string, purposes ...string) (uuid.UUID, string, error) {
	if raw == "" {
		return uuid.Nil, "", invalidLink()
	}
	var userID uuid.UUID
	var purpose string
	err := a.txConn(ctx, func(_ *db.Queries, tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `
			UPDATE auth_tokens
			SET used_at = now()
			WHERE token_hash = $1
			  AND used_at IS NULL
			  AND expires_at > now()
			  AND purpose = ANY($2)
			RETURNING user_id, purpose`, hashToken(raw), purposes).Scan(&userID, &purpose)
		if errors.Is(err, pgx.ErrNoRows) {
			return invalidLink()
		}
		return err
	})
	return userID, purpose, err
}

func invalidLink() error {
	return E(400, "bad_token", "Tautan ini tidak berlaku. Minta yang baru ya.")
}

func (a *App) absolute(path, token string) string {
	return strings.TrimRight(a.Cfg.PublicOrigin, "/") + path + "?token=" + token
}

func (a *App) sendMail(ctx context.Context, to, subject, text string) error {
	if a.Mail == nil {
		return errors.New("mailer is not configured")
	}
	return a.Mail.Send(ctx, mailMessage(to, subject, text))
}

func clientKey(ip string) string {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return "unknown"
	}
	return ip
}
