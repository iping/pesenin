package service

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"pesenin.product.biz.id/api/internal/authlimit"
	"pesenin.product.biz.id/api/internal/config"
	"pesenin.product.biz.id/api/internal/mailer"
	"pesenin.product.biz.id/api/internal/password"
	"pesenin.product.biz.id/api/internal/storage"
	"pesenin.product.biz.id/api/internal/wasender"
)

func testApp(t *testing.T) (*App, *mailer.Capture, *wasender.Capture) {
	t.Helper()
	reset(t)
	pool, err := pgxpool.New(context.Background(), env("TEST_DATABASE_URL", ""))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	cfg := config.Config{
		DevLogin: true, AdminEmail: "admin@pesenin.test",
		PublicOrigin: "http://127.0.0.1:43123", SessionTTL: time.Hour,
		KYCGate: "contact",
	}
	app := New(pool, cfg, storage.Local{Dir: t.TempDir()}, nil)
	mail := &mailer.Capture{}
	wa := &wasender.Capture{}
	app.Mail = mail
	app.WA = wa
	app.Limit = authlimit.New()
	return app, mail, wa
}

func digitsIn(body string) string {
	code := ""
	for _, field := range strings.FieldsFunc(body, func(r rune) bool { return r < '0' || r > '9' }) {
		if len(field) == 6 {
			code = field
		}
	}
	return code
}

func tokenFrom(t *testing.T, body, path string) string {
	t.Helper()
	i := strings.Index(body, path)
	if i < 0 {
		t.Fatalf("missing %s in mail: %s", path, body)
	}
	rest := body[i+len(path):]
	rest = strings.TrimPrefix(rest, "?token=")
	if cut := strings.IndexAny(rest, " \n\r"); cut >= 0 {
		rest = rest[:cut]
	}
	tok, err := url.QueryUnescape(rest)
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func TestSignupDoesNotTakeOverGoogleAccount(t *testing.T) {
	app, mail, _ := testApp(t)
	ctx := context.Background()
	email := "google.user@example.com"
	if _, err := app.UpsertGoogle(ctx, "sub-google-1", email, "Gita", ""); err != nil {
		t.Fatal(err)
	}
	msg, err := app.SignUp(ctx, email, "kucing-oranye", "Penyerang", "203.0.113.8")
	if err != nil || msg != msgSignup {
		t.Fatal(msg, err)
	}
	var hash *string
	if err := app.Pool.QueryRow(ctx, `SELECT password_hash FROM users WHERE email = $1`, email).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	if hash != nil {
		t.Fatal("password was set without email proof")
	}
	if len(mail.Messages) == 0 || !strings.Contains(mail.Messages[len(mail.Messages)-1].Text, "/app/atur-sandi") {
		t.Fatalf("expected a set-password link, got %#v", mail.Messages)
	}
	for _, m := range mail.Messages {
		if strings.Contains(m.Text, "kucing-oranye") {
			t.Fatal("password leaked into mail")
		}
	}
	if _, err := app.LoginPassword(ctx, email, "kucing-oranye", "203.0.113.8"); err == nil {
		t.Fatal("login should fail before the link is used")
	}
	raw := tokenFrom(t, mail.Messages[len(mail.Messages)-1].Text, "/app/atur-sandi")
	if _, err := app.ResetPassword(ctx, raw, "kucing-oranye"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.LoginPassword(ctx, email, "kucing-oranye", "203.0.113.9"); err != nil {
		t.Fatal(err)
	}
}

func TestForgotPasswordResponseDoesNotEnumerate(t *testing.T) {
	app, mail, _ := testApp(t)
	ctx := context.Background()
	missing, err := app.RequestPasswordReset(ctx, "nobody@example.com", "203.0.113.10")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.SignUp(ctx, "ada@example.com", "kucing-oranye", "Ada", "203.0.113.11"); err != nil {
		t.Fatal(err)
	}
	mail.Messages = nil
	present, err := app.RequestPasswordReset(ctx, "ada@example.com", "203.0.113.12")
	if err != nil {
		t.Fatal(err)
	}
	if missing != present || missing != msgReset {
		t.Fatalf("responses differ: %q vs %q", missing, present)
	}
	if len(mail.Messages) != 1 {
		t.Fatalf("mail count %d", len(mail.Messages))
	}
	app.Mail = &mailer.Capture{Err: errors.New("smtp down")}
	again, err := app.RequestPasswordReset(ctx, "ada@example.com", "203.0.113.13")
	silent, serr := app.RequestPasswordReset(ctx, "lain@example.com", "203.0.113.14")
	if err != nil || serr != nil || again != silent {
		t.Fatalf("mail failure leaked: %q %v / %q %v", again, err, silent, serr)
	}
}

func TestVerifyAndResetTokensAreSingleUse(t *testing.T) {
	app, mail, _ := testApp(t)
	ctx := context.Background()
	email := "token@example.com"
	if _, err := app.SignUp(ctx, email, "kucing-oranye", "Tia", "203.0.113.20"); err != nil {
		t.Fatal(err)
	}
	var stored string
	if err := app.Pool.QueryRow(ctx, `SELECT password_hash FROM users WHERE email = $1`, email).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(stored, "$argon2id$") || strings.Contains(stored, "kucing-oranye") {
		t.Fatal("hash was not stored as PHC")
	}
	first := tokenFrom(t, mail.Messages[0].Text, "/app/verifikasi-email")
	mail.Messages = nil
	if _, err := app.SignUp(ctx, email, "kucing-lainnya", "Tia", "203.0.113.21"); err != nil {
		t.Fatal(err)
	}
	second := tokenFrom(t, mail.Messages[0].Text, "/app/verifikasi-email")
	if _, err := app.VerifyEmail(ctx, first); err == nil {
		t.Fatal("old verify token should die when a new one is issued")
	}
	user, err := app.VerifyEmail(ctx, second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.VerifyEmail(ctx, second); err == nil {
		t.Fatal("verify token reused")
	}
	raw, err := app.StartSessionVia(ctx, user.ID, "test", "203.0.113.20", "password")
	if err != nil {
		t.Fatal(err)
	}
	mail.Messages = nil
	if _, err := app.RequestPasswordReset(ctx, email, "203.0.113.22"); err != nil {
		t.Fatal(err)
	}
	reset1 := tokenFrom(t, mail.Messages[0].Text, "/app/atur-sandi")
	if _, err := app.RequestPasswordReset(ctx, email, "203.0.113.23"); err != nil {
		t.Fatal(err)
	}
	reset2 := tokenFrom(t, mail.Messages[1].Text, "/app/atur-sandi")
	if _, err := app.ResetPassword(ctx, reset1, "kucing-baru-sekali"); err == nil {
		t.Fatal("old reset token should die")
	}
	if _, err := app.ResetPassword(ctx, reset2, "kucing-baru-sekali"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.UserFromSession(ctx, raw); err == nil {
		t.Fatal("reset should revoke sessions")
	}
	if _, err := app.ResetPassword(ctx, reset2, "kucing-baru-sekali"); err == nil {
		t.Fatal("reset token reused")
	}
	if _, err := app.Pool.Exec(ctx, `UPDATE auth_tokens SET expires_at = now() - interval '1 minute', used_at = NULL WHERE user_id = $1`, user.ID); err != nil {
		t.Fatal(err)
	}
	var expired string
	if err := app.Pool.QueryRow(ctx, `SELECT token_hash FROM auth_tokens WHERE user_id = $1 LIMIT 1`, user.ID).Scan(&expired); err != nil {
		t.Fatal(err)
	}
	if _, err := app.ResetPassword(ctx, "not-the-raw-token", "kucing-baru-sekali"); err == nil {
		t.Fatal("expired or unknown token should fail")
	}
}

func TestLoginRehashesAndRateLimits(t *testing.T) {
	app, mail, _ := testApp(t)
	ctx := context.Background()
	email := "rehash@example.com"
	old, err := password.HashWith("kucing-oranye", password.Params{Memory: 32, Time: 1, Threads: 1, KeyLen: 32, SaltLen: 16})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.SignUp(ctx, email, "kucing-oranye", "Ria", "203.0.113.30"); err != nil {
		t.Fatal(err)
	}
	tok := tokenFrom(t, mail.Messages[0].Text, "/app/verifikasi-email")
	if _, err := app.VerifyEmail(ctx, tok); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Pool.Exec(ctx, `UPDATE users SET password_hash = $2 WHERE email = $1`, email, old); err != nil {
		t.Fatal(err)
	}
	if _, err := app.LoginPassword(ctx, email, "kucing-oranye", "203.0.113.31"); err != nil {
		t.Fatal(err)
	}
	var stored string
	if err := app.Pool.QueryRow(ctx, `SELECT password_hash FROM users WHERE email = $1`, email).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stored, "m=19456,t=2,p=1") {
		t.Fatal(stored)
	}
	app.limits = &authQuota{
		loginIP: 20, loginEmail: 20, loginWindow: time.Hour,
		signupIP: 20, signupEmail: 20, signupWindow: time.Hour,
		resetIP: 20, resetEmail: 20, resetWindow: time.Hour,
		failMax: 2, failLock: time.Hour, otpPerHour: 5, otpCooldown: time.Minute,
	}
	if _, err := app.LoginPassword(ctx, email, "salah-sekali", "203.0.113.40"); err == nil {
		t.Fatal("expected bad password")
	}
	if _, err := app.LoginPassword(ctx, email, "salah-sekali", "203.0.113.40"); err == nil {
		t.Fatal("expected bad password")
	}
	_, err = app.LoginPassword(ctx, email, "kucing-oranye", "203.0.113.42")
	var se *Error
	if !errors.As(err, &se) || se.Status != 429 {
		t.Fatalf("expected lockout, got %v", err)
	}
	if _, err := app.LoginPassword(ctx, "missing@example.com", "salah-sekali", "198.51.100.2"); err == nil {
		t.Fatal("expected bad password for missing account")
	}
	if _, err := app.LoginPassword(ctx, "missing@example.com", "salah-sekali", "198.51.100.2"); err == nil {
		t.Fatal("expected bad password for missing account")
	}
	_, err = app.LoginPassword(ctx, "missing@example.com", "salah-sekali", "198.51.100.2")
	if !errors.As(err, &se) || se.Status != 429 || se.Code != "rate_limited" {
		t.Fatalf("lockout should look the same for a missing account, got %v", err)
	}
}

func TestWhatsAppOTPAndContactGate(t *testing.T) {
	app, mail, wa := testApp(t)
	app.limits = &authQuota{
		loginIP: 20, loginEmail: 20, loginWindow: time.Hour,
		signupIP: 20, signupEmail: 20, signupWindow: time.Hour,
		resetIP: 20, resetEmail: 20, resetWindow: time.Hour,
		failMax: 5, failLock: time.Minute, otpPerHour: 5, otpCooldown: 0,
	}
	ctx := context.Background()
	email := "otp@example.com"
	if _, err := app.SignUp(ctx, email, "kucing-oranye", "Opi", "203.0.113.50"); err != nil {
		t.Fatal(err)
	}
	user, err := app.VerifyEmail(ctx, tokenFrom(t, mail.Messages[0].Text, "/app/verifikasi-email"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := app.EnsureSellerAccount(ctx, user); err != nil {
		t.Fatal(err)
	}
	brand, _, err := app.CreateBrand(ctx, user.ID, NewBrand{Name: "Toko OTP", Slug: "toko-otp"})
	if err != nil {
		t.Fatal(err)
	}
	st, _, err := app.BrandKYC(ctx, brand.ID)
	if err != nil || st != "unverified" {
		t.Fatal(st, err)
	}
	if err := app.GateBrand(ctx, brand.ID); err == nil {
		t.Fatal("expected the commerce gate to block an unverified brand")
	}
	if err := app.RequestWhatsAppOTP(ctx, user, "081234560099", "203.0.113.50"); err != nil {
		t.Fatal(err)
	}
	if len(wa.Messages) != 1 {
		t.Fatal(wa.Messages)
	}
	code := digitsIn(wa.Messages[0].Body)
	if code == "" {
		t.Fatal(wa.Messages[0])
	}
	var storedHash string
	if err := app.Pool.QueryRow(ctx, `SELECT code_hash FROM whatsapp_otps WHERE user_id = $1`, user.ID).Scan(&storedHash); err != nil {
		t.Fatal(err)
	}
	if storedHash == code || strings.Contains(storedHash, code) {
		t.Fatal("otp stored in the clear")
	}
	for i := 0; i < 5; i++ {
		err = app.CheckWhatsAppOTP(ctx, user, "000000")
	}
	var se *Error
	if !errors.As(err, &se) || (se.Code != "otp_attempts" && se.Code != "bad_code") {
		t.Fatal(err)
	}
	if err := app.CheckWhatsAppOTP(ctx, user, code); err == nil {
		t.Fatal("spent code should not verify")
	}
	wa.Messages = nil
	if err := app.RequestWhatsAppOTP(ctx, user, "+62 812-3456-0099", "203.0.113.51"); err != nil {
		t.Fatal(err)
	}
	code = digitsIn(wa.Messages[0].Body)
	if err := app.CheckWhatsAppOTP(ctx, user, code); err != nil {
		t.Fatal(err)
	}
	st, _, err = app.BrandKYC(ctx, brand.ID)
	if err != nil || st != "contact_verified" {
		t.Fatal(st, err)
	}
	if err := app.GateBrand(ctx, brand.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := app.ChangeEmail(ctx, user, "otp-baru@example.com", "kucing-oranye"); err != nil {
		t.Fatal(err)
	}
	st, _, err = app.BrandKYC(ctx, brand.ID)
	if err != nil || st != "unverified" {
		t.Fatalf("email change should drop contact verification, got %s %v", st, err)
	}
}

func TestKYCReviewAndBadge(t *testing.T) {
	app, user, batch := fixture(t)
	app.Cfg.KYCGate = "contact"
	ctx := context.Background()
	if err := app.GateBrand(ctx, batch.BrandID); err == nil {
		t.Fatal("fixture seller is unverified")
	}
	if _, err := app.CreateTopup(ctx, user.ID, 50000, "kyc-block"); err == nil {
		t.Fatal("topup should be blocked")
	}
	if _, err := app.Pool.Exec(ctx, `
		UPDATE users
		SET email_verified_at = now(), whatsapp_e164 = $2, whatsapp_verified_at = now()
		WHERE id = $1`, user.ID, "+6281111110001"); err != nil {
		t.Fatal(err)
	}
	if err := app.SyncContactKYC(ctx, user.ID); err != nil {
		t.Fatal(err)
	}
	if err := app.SubmitBrandKYC(ctx, user, batch.BrandID); err != nil {
		t.Fatal(err)
	}
	admin := mustAdmin(t, app)
	if err := app.ReviewBrand(ctx, admin, batch.BrandID, "reject", ""); err == nil {
		t.Fatal("reject needs a reason")
	}
	if err := app.ReviewBrand(ctx, admin, batch.BrandID, "approve", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := app.CreateTopup(ctx, user.ID, 50000, "kyc-open"); err != nil {
		t.Fatal(err)
	}
	if !app.BrandBadge(ctx, batch.BrandID) {
		t.Fatal("expected a verified badge")
	}
	if err := app.ReviewBrand(ctx, admin, batch.BrandID, "suspend", "Uji penangguhan"); err != nil {
		t.Fatal(err)
	}
	if app.BrandBadge(ctx, batch.BrandID) {
		t.Fatal("suspended brand should not show the badge")
	}
	if err := app.GateBrand(ctx, batch.BrandID); err == nil {
		t.Fatal("suspended brand should not take orders")
	}
	events, err := app.KYCEvents(ctx, batch.BrandID)
	if err != nil || len(events) < 2 {
		t.Fatalf("events %d %v", len(events), err)
	}
}
