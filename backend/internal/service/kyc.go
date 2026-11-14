package service

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"pesenin.product.biz.id/api/internal/db"
	"pesenin.product.biz.id/api/internal/kyc"
)

// ContactView is the seller checklist. It never includes secrets.
type ContactView struct {
	EmailVerified    bool
	Whatsapp         string
	WhatsappVerified bool
	HasPassword      bool
	Gate             bool
}

// BrandKYCView is one brand's verification status.
type BrandKYCView struct {
	ID        uuid.UUID
	Name      string
	Slug      string
	Status    string
	Reason    string
	UpdatedAt time.Time
}

// KYCEvent is one status change.
type KYCEvent struct {
	From      string
	To        string
	Reason    string
	ActorID   *uuid.UUID
	CreatedAt time.Time
}

func (a *App) ContactOf(ctx context.Context, user db.User) (ContactView, error) {
	secret, err := a.loadSecret(ctx, user.ID)
	if err != nil {
		return ContactView{}, err
	}
	wa := ""
	if secret.Whatsapp != nil {
		wa = *secret.Whatsapp
	}
	return ContactView{
		EmailVerified: secret.emailVerified(), Whatsapp: wa, WhatsappVerified: secret.waVerified(),
		HasPassword: secret.hasPassword(), Gate: a.Cfg.KYCEnforced(),
	}, nil
}

func (a *App) contactReady(ctx context.Context, userID uuid.UUID) (bool, error) {
	secret, err := a.loadSecret(ctx, userID)
	if err != nil {
		return false, err
	}
	return secret.ready(), nil
}

// SyncContactKYC promotes or demotes brands when email or WhatsApp verification changes.
func (a *App) SyncContactKYC(ctx context.Context, userID uuid.UUID) error {
	ready, err := a.contactReady(ctx, userID)
	if err != nil {
		return err
	}
	acc, err := a.Q.GetAccountByOwner(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	brands, err := a.Q.ListBrandsByAccount(ctx, acc.ID)
	if err != nil {
		return err
	}
	for _, b := range brands {
		st, _, err := a.BrandKYC(ctx, b.ID)
		if err != nil {
			return err
		}
		next, changed := kyc.PromoteContact(st, ready)
		if !changed {
			continue
		}
		reason := ""
		if next == kyc.Unverified {
			reason = "Email atau WhatsApp belum terverifikasi."
		}
		if err := a.applyKYC(ctx, b.ID, st, next, reason, nil); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) BrandKYC(ctx context.Context, id uuid.UUID) (string, string, error) {
	var status, reason string
	err := a.Pool.QueryRow(ctx, `SELECT kyc_status, kyc_reason FROM brands WHERE id = $1`, id).Scan(&status, &reason)
	return status, reason, err
}

func (a *App) BrandKYCViews(ctx context.Context, accountID uuid.UUID) ([]BrandKYCView, error) {
	rows, err := a.Pool.Query(ctx, `
		SELECT id, name, slug, kyc_status, kyc_reason, kyc_updated_at
		FROM brands
		WHERE account_id = $1 AND status <> 'archived'
		ORDER BY created_at`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BrandKYCView
	for rows.Next() {
		var v BrandKYCView
		if err := rows.Scan(&v.ID, &v.Name, &v.Slug, &v.Status, &v.Reason, &v.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// SubmitBrandKYC moves contact_verified (or rejected) to pending_review.
func (a *App) SubmitBrandKYC(ctx context.Context, user db.User, brandID uuid.UUID) error {
	if _, _, err := a.OwnedBrand(ctx, user.ID, brandID); err != nil {
		return err
	}
	ready, err := a.contactReady(ctx, user.ID)
	if err != nil {
		return err
	}
	st, _, err := a.BrandKYC(ctx, brandID)
	if err != nil {
		return err
	}
	next, err := kyc.Submit(st, ready)
	if err != nil {
		return E(409, "kyc_state", "Verifikasi email dan WhatsApp dulu, lalu ajukan tinjauan.")
	}
	return a.applyKYC(ctx, brandID, st, next, "", &user.ID)
}

type kycBrandRow struct {
	BrandKYCView
	OwnerEmail    string
	OwnerWhatsapp string
	EmailVerified bool
	WAVerified    bool
	AccountID     uuid.UUID
}

// ListKYCBrands is the Office Pesenin queue.
func (a *App) ListKYCBrands(ctx context.Context, status string) ([]kycBrandRow, error) {
	status = strings.TrimSpace(status)
	if status != "" && !knownKYC(status) {
		return nil, E(400, "bad_status", "Status verifikasi tidak dikenal.")
	}
	rows, err := a.Pool.Query(ctx, `
		SELECT b.id, b.name, b.slug, b.kyc_status, b.kyc_reason, b.kyc_updated_at,
		       b.account_id, u.email,
		       COALESCE(u.whatsapp_e164, ''),
		       u.email_verified_at IS NOT NULL,
		       u.whatsapp_verified_at IS NOT NULL
		FROM brands b
		JOIN accounts a ON a.id = b.account_id
		JOIN users u ON u.id = a.owner_user_id
		WHERE ($1 = '' OR b.kyc_status = $1)
		  AND b.status <> 'archived'
		ORDER BY b.kyc_updated_at DESC
		LIMIT 100`, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []kycBrandRow
	for rows.Next() {
		var v kycBrandRow
		if err := rows.Scan(&v.ID, &v.Name, &v.Slug, &v.Status, &v.Reason, &v.UpdatedAt, &v.AccountID, &v.OwnerEmail, &v.OwnerWhatsapp, &v.EmailVerified, &v.WAVerified); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (a *App) KYCEvents(ctx context.Context, brandID uuid.UUID) ([]KYCEvent, error) {
	rows, err := a.Pool.Query(ctx, `
		SELECT from_status, to_status, reason, actor_user_id, created_at
		FROM brand_kyc_events
		WHERE brand_id = $1
		ORDER BY created_at DESC
		LIMIT 50`, brandID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []KYCEvent
	for rows.Next() {
		var ev KYCEvent
		if err := rows.Scan(&ev.From, &ev.To, &ev.Reason, &ev.ActorID, &ev.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}

// ReviewBrand applies an admin decision: approve, reject, or suspend.
func (a *App) ReviewBrand(ctx context.Context, actor db.User, brandID uuid.UUID, decision, reason string) error {
	if err := a.requireAdmin(actor); err != nil {
		return err
	}
	st, _, err := a.BrandKYC(ctx, brandID)
	if errors.Is(err, pgx.ErrNoRows) {
		return E(404, "not_found", "Brand tidak ketemu.")
	}
	if err != nil {
		return err
	}
	var owner uuid.UUID
	if err := a.Pool.QueryRow(ctx, `
		SELECT accounts.owner_user_id FROM brands
		JOIN accounts ON accounts.id = brands.account_id
		WHERE brands.id = $1`, brandID).Scan(&owner); err != nil {
		return err
	}
	ready, err := a.contactReady(ctx, owner)
	if err != nil {
		return err
	}
	var next string
	switch decision {
	case "approve":
		next, err = kyc.Approve(st, ready)
		reason = ""
	case "reject":
		reason, err = cleanReason(reason)
		if err != nil {
			return err
		}
		next, err = kyc.Reject(st)
	case "suspend":
		reason, err = cleanReason(reason)
		if err != nil {
			return err
		}
		next, err = kyc.Suspend(st)
	default:
		return E(400, "bad_decision", "Keputusan tidak dikenal.")
	}
	if err != nil {
		return E(409, "kyc_state", "Status verifikasi ini tidak bisa diubah begitu.")
	}
	return a.applyKYC(ctx, brandID, st, next, reason, &actor.ID)
}

func cleanReason(reason string) (string, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" || utf8.RuneCountInString(reason) > 500 {
		return "", E(400, "need_reason", "Isi alasan, maksimal 500 karakter.")
	}
	return reason, nil
}

func knownKYC(status string) bool {
	switch status {
	case kyc.Unverified, kyc.ContactVerified, kyc.PendingReview, kyc.Verified, kyc.Rejected, kyc.Suspended:
		return true
	default:
		return false
	}
}

func (a *App) applyKYC(ctx context.Context, brandID uuid.UUID, from, to, reason string, actor *uuid.UUID) error {
	if from == to {
		return nil
	}
	return a.txConn(ctx, func(q *db.Queries, tx pgx.Tx) error {
		var accountID uuid.UUID
		err := tx.QueryRow(ctx, `SELECT account_id FROM brands WHERE id = $1`, brandID).Scan(&accountID)
		if err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `
			UPDATE brands
			SET kyc_status = $2, kyc_reason = $3, kyc_updated_at = now()
			WHERE id = $1 AND kyc_status = $4`, brandID, to, reason, from)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return E(409, "kyc_conflict", "Status verifikasi baru saja berubah. Muat ulang ya.")
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO brand_kyc_events (brand_id, from_status, to_status, reason, actor_user_id)
			VALUES ($1, $2, $3, $4, $5)`, brandID, from, to, reason, actor); err != nil {
			return err
		}
		a.audit(ctx, q, &accountID, actor, "kyc."+to, "brand", brandID.String(),
			map[string]string{"status": from}, map[string]string{"status": to, "reason": reason})
		return nil
	})
}

// GateBrand blocks a paid order when the seller is not contact-verified.
// Buyers see a generic message.
func (a *App) GateBrand(ctx context.Context, brandID uuid.UUID) error {
	if !a.Cfg.KYCEnforced() {
		return nil
	}
	var owner uuid.UUID
	err := a.Pool.QueryRow(ctx, `
		SELECT accounts.owner_user_id FROM brands
		JOIN accounts ON accounts.id = brands.account_id
		WHERE brands.id = $1`, brandID).Scan(&owner)
	if err != nil {
		return err
	}
	if err := a.SyncContactKYC(ctx, owner); err != nil {
		return err
	}
	ready, err := a.contactReady(ctx, owner)
	if err != nil {
		return err
	}
	st, _, err := a.BrandKYC(ctx, brandID)
	if err != nil {
		return err
	}
	if kyc.CanTransact(st, ready, true) {
		return nil
	}
	return E(403, "kyc_required", "Toko ini belum bisa terima pesanan. Coba lagi nanti ya.")
}

// GateTopup blocks koin top-up until the seller's contact is verified and a brand can transact.
func (a *App) GateTopup(ctx context.Context, userID uuid.UUID) error {
	if !a.Cfg.KYCEnforced() {
		return nil
	}
	if err := a.SyncContactKYC(ctx, userID); err != nil {
		return err
	}
	ready, err := a.contactReady(ctx, userID)
	if err != nil {
		return err
	}
	if !ready {
		return E(403, "kyc_required", "Verifikasi email dan WhatsApp dulu sebelum isi Koin.")
	}
	acc, err := a.Q.GetAccountByOwner(ctx, userID)
	if err != nil {
		return E(403, "kyc_required", "Verifikasi kontak toko dulu sebelum isi Koin.")
	}
	brands, err := a.Q.ListBrandsByAccount(ctx, acc.ID)
	if err != nil {
		return err
	}
	if len(brands) == 0 {
		return E(403, "kyc_required", "Buat toko dan verifikasi kontak dulu sebelum isi Koin.")
	}
	ok := false
	for _, b := range brands {
		st, _, err := a.BrandKYC(ctx, b.ID)
		if err != nil {
			return err
		}
		if st == kyc.Suspended {
			return E(403, "kyc_suspended", "Akun toko ditangguhkan.")
		}
		if kyc.CanTransact(st, true, true) {
			ok = true
		}
	}
	if !ok {
		return E(403, "kyc_required", "Verifikasi kontak toko dulu sebelum isi Koin.")
	}
	return nil
}

// BrandBadge is true when Office Pesenin has verified the brand and contact still holds.
func (a *App) BrandBadge(ctx context.Context, brandID uuid.UUID) bool {
	var owner uuid.UUID
	var status string
	err := a.Pool.QueryRow(ctx, `
		SELECT b.kyc_status, accounts.owner_user_id
		FROM brands b
		JOIN accounts ON accounts.id = b.account_id
		WHERE b.id = $1`, brandID).Scan(&status, &owner)
	if err != nil || status != kyc.Verified {
		return false
	}
	ready, err := a.contactReady(ctx, owner)
	return err == nil && ready
}
