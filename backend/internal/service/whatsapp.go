package service

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"pesenin.product.biz.id/api/internal/db"
	"pesenin.product.biz.id/api/internal/otp"
	"pesenin.product.biz.id/api/internal/text"
	"pesenin.product.biz.id/api/internal/wasender"
)

// RequestWhatsAppOTP stores a hashed 6-digit code and asks the sender to deliver it.
// A different number clears WhatsApp verification immediately.
func (a *App) RequestWhatsAppOTP(ctx context.Context, user db.User, phone, ip string) error {
	e164, err := text.WhatsApp(phone)
	if err != nil {
		return E(400, "bad_wa", "Nomor WhatsApp belum pas. Pakai format 08… atau +62…")
	}
	ip = clientKey(ip)
	q := a.quota()
	if a.Limit != nil {
		if !a.Limit.Allow("otp:ip:"+ip, q.otpPerHour, time.Hour) || !a.Limit.Allow("otp:user:"+user.ID.String(), q.otpPerHour, time.Hour) || !a.Limit.Allow("otp:phone:"+e164, q.otpPerHour, time.Hour) {
			return E(429, "rate_limited", "Terlalu banyak kode. Coba lagi sebentar ya.")
		}
	}
	var other uuid.UUID
	err = a.Pool.QueryRow(ctx, `SELECT id FROM users WHERE whatsapp_e164 = $1 AND id <> $2`, e164, user.ID).Scan(&other)
	if err == nil {
		return E(409, "wa_taken", "Nomor WhatsApp ini sudah dipakai akun lain.")
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	secret, err := a.loadSecret(ctx, user.ID)
	if err != nil {
		return err
	}
	if secret.Whatsapp != nil && *secret.Whatsapp == e164 && secret.waVerified() {
		return E(400, "wa_verified", "WhatsApp ini sudah terverifikasi.")
	}
	var last time.Time
	err = a.Pool.QueryRow(ctx, `
		SELECT created_at FROM whatsapp_otps
		WHERE user_id = $1 ORDER BY created_at DESC LIMIT 1`, user.ID).Scan(&last)
	if err == nil && q.otpCooldown > 0 && time.Since(last) < q.otpCooldown {
		return E(429, "otp_cooldown", "Tunggu sebentar sebelum kirim kode lagi.")
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	code, err := otp.New()
	if err != nil {
		return err
	}
	if _, err := a.Pool.Exec(ctx, `
		UPDATE users
		SET whatsapp_e164 = $2,
		    whatsapp_verified_at = CASE WHEN whatsapp_e164 IS DISTINCT FROM $2 THEN NULL ELSE whatsapp_verified_at END
		WHERE id = $1`, user.ID, e164); err != nil {
		if isUnique(err, "") {
			return E(409, "wa_taken", "Nomor WhatsApp ini sudah dipakai akun lain.")
		}
		return err
	}
	if _, err := a.Pool.Exec(ctx, `
		UPDATE whatsapp_otps SET consumed_at = now()
		WHERE user_id = $1 AND consumed_at IS NULL`, user.ID); err != nil {
		return err
	}
	if _, err := a.Pool.Exec(ctx, `
		INSERT INTO whatsapp_otps (user_id, phone_e164, code_hash, salt, expires_at, max_attempts)
		VALUES ($1, $2, $3, $4, $5, 5)`, user.ID, e164, code.Hash, code.Salt, time.Now().Add(otpTTL)); err != nil {
		return err
	}
	if secret.Whatsapp == nil || *secret.Whatsapp != e164 || secret.waVerified() {
		if err := a.SyncContactKYC(ctx, user.ID); err != nil {
			return err
		}
	}
	body := "Kode Pesenin kamu: " + code.Plain + ". Berlaku 5 menit. Jangan berikan ke siapa pun."
	if a.WA == nil {
		a.WA = wasender.Log{}
	}
	if err := a.WA.Send(ctx, wasender.Message{To: e164, Body: body}); err != nil {
		_, _ = a.Pool.Exec(ctx, `
			UPDATE whatsapp_otps SET consumed_at = now()
			WHERE user_id = $1 AND code_hash = $2 AND consumed_at IS NULL`, user.ID, code.Hash)
		slog.Error("whatsapp otp", "err", err)
		return E(503, "wa_unavailable", "Kode WhatsApp belum terkirim. Coba lagi ya.")
	}
	return nil
}

// CheckWhatsAppOTP confirms a code and marks the number verified.
// A wrong code still commits the attempt so a rollback cannot reset the counter.
func (a *App) CheckWhatsAppOTP(ctx context.Context, user db.User, code string) error {
	code = strings.TrimSpace(code)
	if len(code) != otp.Digits {
		return E(400, "bad_code", "Kode WhatsApp 6 digit.")
	}
	var (
		mismatch bool
		tooMany  bool
		expired  bool
	)
	err := a.txConn(ctx, func(_ *db.Queries, tx pgx.Tx) error {
		var id uuid.UUID
		var phone, hash, salt string
		var expires time.Time
		var attempts, max int
		err := tx.QueryRow(ctx, `
			SELECT id, phone_e164, code_hash, salt, expires_at, attempts, max_attempts
			FROM whatsapp_otps
			WHERE user_id = $1 AND consumed_at IS NULL
			ORDER BY created_at DESC
			LIMIT 1
			FOR UPDATE`, user.ID).Scan(&id, &phone, &hash, &salt, &expires, &attempts, &max)
		if errors.Is(err, pgx.ErrNoRows) {
			return E(400, "bad_code", "Minta kode dulu ya.")
		}
		if err != nil {
			return err
		}
		if attempts >= max {
			tooMany = true
			return nil
		}
		if time.Now().After(expires) {
			expired = true
			_, err = tx.Exec(ctx, `UPDATE whatsapp_otps SET consumed_at = now() WHERE id = $1`, id)
			return err
		}
		if !otp.Match(salt, code, hash) {
			mismatch = true
			attempts++
			if attempts >= max {
				tooMany = true
			}
			_, err = tx.Exec(ctx, `UPDATE whatsapp_otps SET attempts = $2 WHERE id = $1`, id, attempts)
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE whatsapp_otps SET consumed_at = now() WHERE id = $1`, id); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `
			UPDATE users
			SET whatsapp_e164 = $2, whatsapp_verified_at = now()
			WHERE id = $1 AND (whatsapp_e164 = $2 OR whatsapp_e164 IS NULL)`, user.ID, phone)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return E(409, "wa_changed", "Nomor WhatsApp sudah berubah. Minta kode baru ya.")
		}
		return nil
	})
	if err != nil {
		return err
	}
	if tooMany {
		return E(429, "otp_attempts", "Terlalu banyak percobaan. Minta kode baru ya.")
	}
	if expired {
		return E(400, "otp_expired", "Kode kedaluwarsa. Minta yang baru ya.")
	}
	if mismatch {
		return E(400, "bad_code", "Kode belum cocok.")
	}
	return a.SyncContactKYC(ctx, user.ID)
}
