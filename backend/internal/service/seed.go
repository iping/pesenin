package service

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"pesenin.product.biz.id/api/internal/db"
)

// SeedDemo creates a super admin and the Dapur Senja sample seller.
// It is idempotent and only runs when SEED_DEMO=true.
func (a *App) SeedDemo(ctx context.Context) error {
	if _, err := a.Q.GetUserByEmail(ctx, a.Cfg.AdminEmail); err != nil {
		if _, err := a.Q.CreateUser(ctx, db.CreateUserParams{
			Email: a.Cfg.AdminEmail, Name: "Admin Pesenin", IsPlatformAdmin: true,
		}); err != nil {
			return err
		}
		slog.Info("seeded platform admin", "email", a.Cfg.AdminEmail)
	}
	_, _ = a.Pool.Exec(ctx, `UPDATE users SET email_verified_at = COALESCE(email_verified_at, now()) WHERE email = $1`, a.Cfg.AdminEmail)
	const sellerEmail = "rini@dapur.test"
	user, err := a.Q.GetUserByEmail(ctx, sellerEmail)
	if err != nil {
		user, err = a.Q.CreateUser(ctx, db.CreateUserParams{Email: sellerEmail, Name: "Rini Lestari"})
		if err != nil {
			return err
		}
	}
	if _, _, _, err := a.EnsureSellerAccount(ctx, user); err != nil {
		return err
	}
	acc, err := a.Q.GetAccountByOwner(ctx, user.ID)
	if err != nil {
		return err
	}
	brands, err := a.Q.ListBrandsByAccount(ctx, acc.ID)
	if err != nil {
		return err
	}
	if len(brands) > 0 {
		return a.markDemoVerified(ctx, user.ID, brands[0].ID, "+6281234560001")
	}
	brand, _, err := a.CreateBrand(ctx, user.ID, NewBrand{
		Name: "Dapur Senja", Slug: "dapur-senja",
		Description: "Salt bread dan kopi susu dari rumah.", WA: "081234560001",
	})
	if err != nil {
		return err
	}
	if _, err := a.AddBank(ctx, user.ID, brand.ID, BankIn{
		BankCode: "BCA", AccountNumber: "5230881234", HolderName: "Rini Lestari", IsDefault: true,
	}); err != nil {
		return err
	}
	ori, err := a.SaveProduct(ctx, user.ID, brand.ID, nil, ProductIn{
		Name: "Salt Bread Original", Description: "Butter Prancis, garam laut, renyah di luar", Price: 15000,
	})
	if err != nil {
		return err
	}
	if _, err := a.SaveProduct(ctx, user.ID, brand.ID, nil, ProductIn{
		Name: "Salt Bread Keju", Description: "Isi keju cheddar leleh", Price: 17500,
	}); err != nil {
		return err
	}
	if _, err := a.SaveProduct(ctx, user.ID, brand.ID, nil, ProductIn{
		Name: "Kopi Susu Gula Aren", Description: "Botol 250 ml, gula aren asli", Price: 15000,
	}); err != nil {
		return err
	}
	if _, err := a.SaveProduct(ctx, user.ID, brand.ID, nil, ProductIn{
		Name: "Salt Bread Cokelat", Description: "Dark chocolate 54%", Price: 18000,
	}); err != nil {
		return err
	}
	if _, err := a.SaveCharge(ctx, user.ID, brand.ID, nil, ChargeIn{
		Name: "Packing", Amount: 3000, Methods: []string{"pickup", "delivery"}, Active: true,
	}); err != nil {
		return err
	}
	if _, err := a.SaveCharge(ctx, user.ID, brand.ID, nil, ChargeIn{
		Name: "Ongkir", Amount: 10000, Methods: []string{"delivery"}, Active: true, Sort: 1,
	}); err != nil {
		return err
	}
	stores, err := a.Q.ListStores(ctx, brand.ID)
	if err != nil {
		return err
	}
	addr := "Pos Satpam Cluster Melati"
	if _, err := a.UpdateStore(ctx, user.ID, stores[0].ID, StorePatch{
		Name: "Utama", Address: addr, IsOpen: true, AcceptPreordersWhenClosed: true,
	}); err != nil {
		return err
	}
	products, err := a.Q.ListProducts(ctx, brand.ID)
	if err != nil {
		return err
	}
	now := time.Now()
	var items []BatchItemIn
	for _, p := range products {
		q := int32(30)
		if p.ID == ori.ID {
			q = 40
		}
		items = append(items, BatchItemIn{ProductID: p.ID, Price: p.BasePrice, Quota: &q})
	}
	if _, err := a.CreateBatch(ctx, user.ID, brand.ID, BatchIn{
		Title: "Salt bread fresh Sabtu pagi", OpensAt: now.Add(-2 * time.Hour), ClosesAt: now.Add(48 * time.Hour),
		ReadyAt: now.Add(72 * time.Hour), QuotaTotal: int32ptr(50), AllowPickup: true, AllowDelivery: true,
		Publish: true, Notes: "Ambil di pos satpam, Sabtu pagi.", Items: items,
	}); err != nil {
		return err
	}
	slog.Info("seeded demo seller", "email", sellerEmail, "slug", "dapur-senja")
	return a.markDemoVerified(ctx, user.ID, brand.ID, "+6281234560001")
}

func int32ptr(n int32) *int32 { return &n }

func (a *App) markDemoVerified(ctx context.Context, userID, brandID uuid.UUID, phone string) error {
	_, err := a.Pool.Exec(ctx, `
		UPDATE users
		SET email_verified_at = COALESCE(email_verified_at, now()),
		    whatsapp_e164 = COALESCE(whatsapp_e164, $2),
		    whatsapp_verified_at = COALESCE(whatsapp_verified_at, now())
		WHERE id = $1`, userID, phone)
	if err != nil {
		return err
	}
	tag, err := a.Pool.Exec(ctx, `
		UPDATE brands
		SET kyc_status = 'verified', kyc_reason = '', kyc_updated_at = now()
		WHERE id = $1 AND kyc_status = 'unverified'`, brandID)
	if err != nil || tag.RowsAffected() == 0 {
		return err
	}
	_, err = a.Pool.Exec(ctx, `
		INSERT INTO brand_kyc_events (brand_id, from_status, to_status, reason)
		VALUES ($1, 'unverified', 'verified', 'Demo seed')`, brandID)
	return err
}

// silence unused in case seed imports shift
var _ = uuid.Nil
