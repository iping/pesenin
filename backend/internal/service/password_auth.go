package service

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5"

	"pesenin.product.biz.id/api/internal/db"
	"pesenin.product.biz.id/api/internal/password"
)

// SignUp creates an unverified password account, or emails an existing account
// without changing it. The HTTP response is the same in every success path.
func (a *App) SignUp(ctx context.Context, email, pw, name, ip string) (string, error) {
	email, err := normalizeEmail(email)
	if err != nil {
		return "", err
	}
	ip = clientKey(ip)
	q := a.quota()
	if err := a.allow("signup:ip:"+ip, "signup:email:"+email, q.signupIP, q.signupEmail, q.signupWindow); err != nil {
		return "", err
	}
	if err := password.Check(pw); err != nil {
		var pe *password.PolicyError
		if errors.As(err, &pe) {
			return "", E(400, "bad_password", pe.Message)
		}
		return "", E(400, "bad_password", "Kata sandi belum pas.")
	}
	name, err = cleanName(name, email)
	if err != nil {
		return "", err
	}
	// Hash even when the address already exists so the two paths take similar time.
	hash, err := password.Hash(pw)
	if err != nil {
		return "", err
	}
	user, err := a.Q.GetUserByEmail(ctx, email)
	if errors.Is(err, pgx.ErrNoRows) {
		created, cerr := a.insertPasswordUser(ctx, email, name, hash)
		if isUnique(cerr, "users_email_key") || isUnique(cerr, "") {
			user, err = a.Q.GetUserByEmail(ctx, email)
		} else if cerr != nil {
			return "", cerr
		} else {
			a.noteMail(a.mailVerify(ctx, created))
			return msgSignup, nil
		}
	}
	if err != nil {
		return "", err
	}
	secret, err := a.loadSecret(ctx, user.ID)
	if err != nil {
		return "", err
	}
	switch {
	case !secret.hasPassword():
		a.noteMail(a.mailLink(ctx, user))
	case !secret.emailVerified():
		a.noteMail(a.mailVerify(ctx, user))
	default:
		a.noteMail(a.mailAlready(ctx, user))
	}
	return msgSignup, nil
}

func (a *App) insertPasswordUser(ctx context.Context, email, name, hash string) (db.User, error) {
	var user db.User
	err := a.txConn(ctx, func(q *db.Queries, tx pgx.Tx) error {
		var err error
		user, err = q.CreateUser(ctx, db.CreateUserParams{Email: email, Name: name})
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE users SET password_hash = $2 WHERE id = $1`, user.ID, hash)
		return err
	})
	return user, err
}

func (a *App) noteMail(err error) {
	if err != nil {
		slog.Error("auth mail", "err", err)
	}
}

// LoginPassword checks the password, rehashes when parameters changed, and
// refuses unverified emails. Failures use one message.
func (a *App) LoginPassword(ctx context.Context, email, pw, ip string) (db.User, error) {
	email, err := normalizeEmail(email)
	if err != nil {
		return db.User{}, err
	}
	if pw == "" || len(pw) > 1024 {
		return db.User{}, E(401, "bad_login", "Email atau kata sandi belum cocok.")
	}
	ip = clientKey(ip)
	if a.loginLocked(email, ip) {
		return db.User{}, E(429, "rate_limited", "Terlalu banyak percobaan. Coba lagi sebentar ya.")
	}
	q := a.quota()
	if err := a.allow("login:ip:"+ip, "login:email:"+email, q.loginIP, q.loginEmail, q.loginWindow); err != nil {
		return db.User{}, err
	}
	user, err := a.Q.GetUserByEmail(ctx, email)
	if errors.Is(err, pgx.ErrNoRows) {
		a.burnPassword(pw)
		a.noteLoginFail(email, ip)
		return db.User{}, E(401, "bad_login", "Email atau kata sandi belum cocok.")
	}
	if err != nil {
		return db.User{}, err
	}
	secret, err := a.loadSecret(ctx, user.ID)
	if err != nil {
		return db.User{}, err
	}
	if !secret.hasPassword() {
		a.burnPassword(pw)
		a.noteLoginFail(email, ip)
		return db.User{}, E(401, "bad_login", "Email atau kata sandi belum cocok.")
	}
	ok, rehash, err := password.Verify(pw, *secret.PasswordHash)
	if err != nil || !ok {
		a.noteLoginFail(email, ip)
		return db.User{}, E(401, "bad_login", "Email atau kata sandi belum cocok.")
	}
	a.clearLoginFail(email, ip)
	if rehash {
		if next, err := password.Hash(pw); err == nil {
			if _, err := a.Pool.Exec(ctx, `UPDATE users SET password_hash = $2 WHERE id = $1`, user.ID, next); err != nil {
				return db.User{}, err
			}
		}
	}
	if !secret.emailVerified() {
		return db.User{}, E(403, "email_unverified", "Email belum dikonfirmasi. Cek kotak masuk ya.")
	}
	return a.finishLogin(ctx, user)
}

func (a *App) burnPassword(pw string) {
	dummy, err := password.Dummy()
	if err != nil {
		return
	}
	_, _, _ = password.Verify(pw, dummy)
}

// RequestPasswordReset always returns the same message.
// A missing account, a Google-only account, and a password account look the same.
func (a *App) RequestPasswordReset(ctx context.Context, email, ip string) (string, error) {
	email, err := normalizeEmail(email)
	if err != nil {
		return "", err
	}
	ip = clientKey(ip)
	q := a.quota()
	if err := a.allow("reset:ip:"+ip, "reset:email:"+email, q.resetIP, q.resetEmail, q.resetWindow); err != nil {
		return "", err
	}
	user, err := a.Q.GetUserByEmail(ctx, email)
	if errors.Is(err, pgx.ErrNoRows) {
		return msgReset, nil
	}
	if err != nil {
		slog.Error("password reset lookup", "err", err)
		return msgReset, nil
	}
	secret, err := a.loadSecret(ctx, user.ID)
	if err != nil {
		slog.Error("password reset secret", "err", err)
		return msgReset, nil
	}
	var sendErr error
	if secret.hasPassword() {
		sendErr = a.mailReset(ctx, user)
	} else {
		sendErr = a.mailLink(ctx, user)
	}
	if sendErr != nil {
		slog.Error("password reset mail", "err", sendErr)
	}
	return msgReset, nil
}

// ResetPassword consumes a reset or link token, stores a new hash, and
// revokes every existing session.
func (a *App) ResetPassword(ctx context.Context, token, pw string) (db.User, error) {
	if err := password.Check(pw); err != nil {
		var pe *password.PolicyError
		if errors.As(err, &pe) {
			return db.User{}, E(400, "bad_password", pe.Message)
		}
		return db.User{}, E(400, "bad_password", "Kata sandi belum pas.")
	}
	hash, err := password.Hash(pw)
	if err != nil {
		return db.User{}, err
	}
	userID, _, err := a.consumeToken(ctx, token, purposeReset, purposeLink)
	if err != nil {
		return db.User{}, err
	}
	if _, err := a.Pool.Exec(ctx, `
		UPDATE users
		SET password_hash = $2,
		    email_verified_at = COALESCE(email_verified_at, now())
		WHERE id = $1`, userID, hash); err != nil {
		return db.User{}, err
	}
	if err := a.Q.DeleteUserSessions(ctx, userID); err != nil {
		return db.User{}, err
	}
	user, err := a.Q.GetUserByID(ctx, userID)
	if err != nil {
		return db.User{}, err
	}
	if err := a.SyncContactKYC(ctx, user.ID); err != nil {
		return db.User{}, err
	}
	return a.finishLogin(ctx, user)
}

// VerifyEmail consumes a sign-up token and marks the address verified.
func (a *App) VerifyEmail(ctx context.Context, token string) (db.User, error) {
	userID, _, err := a.consumeToken(ctx, token, purposeVerify)
	if err != nil {
		return db.User{}, err
	}
	if err := a.markEmailVerified(ctx, userID); err != nil {
		return db.User{}, err
	}
	user, err := a.Q.GetUserByID(ctx, userID)
	if err != nil {
		return db.User{}, err
	}
	if err := a.SyncContactKYC(ctx, user.ID); err != nil {
		return db.User{}, err
	}
	return a.finishLogin(ctx, user)
}

// ResendVerification always returns the same message.
func (a *App) ResendVerification(ctx context.Context, email, ip string) (string, error) {
	email, err := normalizeEmail(email)
	if err != nil {
		return "", err
	}
	ip = clientKey(ip)
	q := a.quota()
	if err := a.allow("resend:ip:"+ip, "resend:email:"+email, q.resetIP, q.resetEmail, q.resetWindow); err != nil {
		return "", err
	}
	user, err := a.Q.GetUserByEmail(ctx, email)
	if err != nil {
		return msgResend, nil
	}
	secret, err := a.loadSecret(ctx, user.ID)
	if err != nil || secret.emailVerified() {
		return msgResend, nil
	}
	if err := a.mailVerify(ctx, user); err != nil {
		slog.Error("verify mail", "err", err)
	}
	return msgResend, nil
}

// RequestPasswordLink emails a signed-in Google user a one-time set-password link.
func (a *App) RequestPasswordLink(ctx context.Context, user db.User) (string, error) {
	secret, err := a.loadSecret(ctx, user.ID)
	if err != nil {
		return "", err
	}
	if secret.hasPassword() {
		return "", E(400, "has_password", "Akun ini sudah punya kata sandi. Pakai lupa kata sandi kalau perlu ganti.")
	}
	if err := a.mailLink(ctx, user); err != nil {
		slog.Error("link mail", "err", err)
		return "", E(503, "mail_unavailable", "Email belum terkirim. Coba lagi ya.")
	}
	return "Cek email kamu. Tautannya berlaku 30 menit.", nil
}

// ChangeEmail updates the address and clears email verification.
func (a *App) ChangeEmail(ctx context.Context, user db.User, email, currentPassword string) (db.User, error) {
	email, err := normalizeEmail(email)
	if err != nil {
		return db.User{}, err
	}
	secret, err := a.loadSecret(ctx, user.ID)
	if err != nil {
		return db.User{}, err
	}
	if secret.hasPassword() {
		ok, _, err := password.Verify(currentPassword, *secret.PasswordHash)
		if err != nil || !ok {
			return db.User{}, E(401, "bad_login", "Kata sandi belum cocok.")
		}
	}
	if strings.EqualFold(user.Email, email) {
		if secret.emailVerified() {
			return user, nil
		}
		_ = a.mailVerify(ctx, user)
		return user, nil
	}
	tag, err := a.Pool.Exec(ctx, `
		UPDATE users SET email = $2, email_verified_at = NULL
		WHERE id = $1`, user.ID, email)
	if isUnique(err, "") || isUnique(err, "users_email_key") {
		return db.User{}, E(409, "email_taken", "Email ini sudah dipakai.")
	}
	if err != nil {
		return db.User{}, err
	}
	if tag.RowsAffected() == 0 {
		return db.User{}, E(404, "not_found", "Akun tidak ketemu.")
	}
	user, err = a.Q.GetUserByID(ctx, user.ID)
	if err != nil {
		return db.User{}, err
	}
	if err := a.SyncContactKYC(ctx, user.ID); err != nil {
		return db.User{}, err
	}
	if err := a.mailVerify(ctx, user); err != nil {
		slog.Error("verify mail", "err", err)
	}
	return user, nil
}

func (a *App) finishLogin(ctx context.Context, user db.User) (db.User, error) {
	if user.IsPlatformAdmin {
		return user, nil
	}
	if _, _, _, err := a.EnsureSellerAccount(ctx, user); err != nil {
		return db.User{}, err
	}
	return user, nil
}

func (a *App) mailVerify(ctx context.Context, user db.User) error {
	raw, err := a.issueToken(ctx, user.ID, purposeVerify)
	if err != nil {
		return err
	}
	text := "Halo!\n\nPingping di sini. Konfirmasi email kamu untuk Pesenin dengan membuka tautan ini (berlaku 30 menit):\n\n" +
		a.absolute("/app/verifikasi-email", raw) + "\n\nKalau kamu tidak merasa daftar, abaikan email ini.\n"
	return a.sendMail(ctx, user.Email, "Konfirmasi email Pesenin", text)
}

func (a *App) mailReset(ctx context.Context, user db.User) error {
	raw, err := a.issueToken(ctx, user.ID, purposeReset)
	if err != nil {
		return err
	}
	text := "Halo!\n\nAda permintaan untuk atur ulang kata sandi Pesenin. Buka tautan ini (berlaku 30 menit, sekali pakai):\n\n" +
		a.absolute("/app/atur-sandi", raw) + "\n\nKalau bukan kamu, abaikan email ini. Kata sandi tidak berubah.\n"
	return a.sendMail(ctx, user.Email, "Atur ulang kata sandi Pesenin", text)
}

func (a *App) mailLink(ctx context.Context, user db.User) error {
	raw, err := a.issueToken(ctx, user.ID, purposeLink)
	if err != nil {
		return err
	}
	text := "Halo!\n\nAda yang ingin menambahkan kata sandi ke akun Pesenin di email ini. Kalau itu kamu, buka tautan ini (berlaku 30 menit):\n\n" +
		a.absolute("/app/atur-sandi", raw) + "\n\nKalau bukan kamu, abaikan email ini. Masuk dengan Google tetap jalan.\n"
	return a.sendMail(ctx, user.Email, "Buat kata sandi Pesenin", text)
}

func (a *App) mailAlready(ctx context.Context, user db.User) error {
	text := "Halo!\n\nEmail ini sudah punya akun Pesenin. Masuk saja, atau atur ulang kata sandi kalau lupa.\n\n" +
		strings.TrimRight(a.Cfg.PublicOrigin, "/") + "/app/masuk\n"
	return a.sendMail(ctx, user.Email, "Akun Pesenin kamu", text)
}
