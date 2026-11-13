package service

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"pesenin.product.biz.id/api/internal/config"
	"pesenin.product.biz.id/api/internal/db"
	"pesenin.product.biz.id/api/internal/migrate"
	"pesenin.product.biz.id/api/internal/storage"
)

func TestMain(m *testing.M) {
	adminURL := env("TEST_ADMIN_URL", "postgres://pesenin:pesenin@127.0.0.1:54329/pesenin?sslmode=disable")
	testURL := env("TEST_DATABASE_URL", "postgres://pesenin:pesenin@127.0.0.1:54329/pesenin_test?sslmode=disable")
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		fmt.Println("integration tests skipped:", err)
		os.Exit(0)
	}
	_, _ = conn.Exec(ctx, "CREATE DATABASE pesenin_test")
	conn.Close(ctx)
	if err := migrate.Up(testURL); err != nil {
		fmt.Println("migrate test db:", err)
		os.Exit(1)
	}
	_ = os.Setenv("TEST_DATABASE_URL", testURL)
	os.Exit(m.Run())
}

func TestUniqueAmountAllocation(t *testing.T) {
	app, _, batch := fixture(t)
	var totals []int64
	var suffixes []int32
	for i := 0; i < 6; i++ {
		o := mustOrder(t, app, batch.Code, fmt.Sprintf("Pembeli %d", i), fmt.Sprintf("0812000000%d", i))
		if o.UniqueSuffix%100 == 0 {
			t.Fatalf("suffix %d ends in 00", o.UniqueSuffix)
		}
		if o.TotalDue != o.BaseTotal+int64(o.UniqueSuffix) {
			t.Fatalf("total %d", o.TotalDue)
		}
		for _, prev := range totals {
			if prev == o.TotalDue {
				t.Fatal("duplicate total")
			}
		}
		totals = append(totals, o.TotalDue)
		suffixes = append(suffixes, o.UniqueSuffix)
	}
	ctx := context.Background()
	// Expire the first order but keep its total reserved for 48h.
	if _, err := app.Pool.Exec(ctx, `UPDATE orders SET payment_status = 'expired', expires_at = now() WHERE id = $1`, mustID(t, app, totals[0])); err != nil {
		t.Fatal(err)
	}
	_, _ = app.Pool.Exec(ctx, `UPDATE batch_items SET sold_count = sold_count - 1 WHERE batch_id = $1`, batch.ID)
	again := mustOrder(t, app, batch.Code, "Pembeli lagi", "081299988877")
	if again.TotalDue == totals[0] {
		t.Fatal("expired total was reused inside 48h")
	}
	if _, err := app.Pool.Exec(ctx, `UPDATE orders SET expires_at = now() - interval '49 hours' WHERE total_due = $1 AND payment_status = 'expired'`, totals[0]); err != nil {
		t.Fatal(err)
	}
	bank := again.BankAccountID
	reserved, err := app.Q.ListReservedTotals(ctx, bank)
	if err != nil {
		t.Fatal(err)
	}
	for _, tot := range reserved {
		if tot == totals[0] {
			t.Fatal("suffix still reserved after 49h")
		}
	}
	_ = suffixes
}

func TestPaidReleasesSuffix(t *testing.T) {
	app, user, batch := fixture(t)
	o := mustOrder(t, app, batch.Code, "Dimas", "081234567890")
	if _, err := app.MarkPaid(context.Background(), user.ID, o.ID); err != nil {
		t.Fatal(err)
	}
	reserved, err := app.Q.ListReservedTotals(context.Background(), o.BankAccountID)
	if err != nil {
		t.Fatal(err)
	}
	for _, tot := range reserved {
		if tot == o.TotalDue {
			t.Fatal("paid total is still reserved")
		}
	}
}

func TestKoinLedgerAndStateMachine(t *testing.T) {
	app, user, batch := fixture(t)
	ctx := context.Background()
	o := mustOrder(t, app, batch.Code, "Sari", "081211112222")
	if _, err := app.MarkPaid(ctx, user.ID, o.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := app.MarkPaid(ctx, user.ID, o.ID); err == nil {
		t.Fatal("second mark from paid should fail")
	}
	w := walletOf(t, app, user.ID)
	if w.BalanceKoin != 4900 {
		t.Fatalf("balance %d", w.BalanceKoin)
	}
	if n := countLedger(t, app, w.ID, "order_debit"); n != 1 {
		t.Fatalf("debits %d", n)
	}
	if _, err := app.UndoPaid(ctx, user.ID, o.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := app.MarkPaid(ctx, user.ID, o.ID); err != nil {
		t.Fatal(err)
	}
	w = walletOf(t, app, user.ID)
	if w.BalanceKoin != 4900 || countLedger(t, app, w.ID, "order_debit") != 1 {
		t.Fatalf("re-mark charged again: bal %d", w.BalanceKoin)
	}
	if _, err := app.CancelOrder(ctx, user.ID, o.ID); err != nil {
		t.Fatal(err)
	}
	w = walletOf(t, app, user.ID)
	if w.BalanceKoin != 5000 || countLedger(t, app, w.ID, "refund_credit") != 1 {
		t.Fatalf("refund bal %d", w.BalanceKoin)
	}
	if _, err := app.CancelOrder(ctx, user.ID, o.ID); err == nil {
		t.Fatal("cancel twice")
	}

	// Fulfilment before payment is rejected.
	o2 := mustOrder(t, app, batch.Code, "Budi", "081233334444")
	if _, err := app.SetFulfilment(ctx, user.ID, o2.ID, "ready"); err == nil {
		t.Fatal("ready before paid")
	}
	if _, err := app.MarkPaid(ctx, user.ID, o2.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := app.SetFulfilment(ctx, user.ID, o2.ID, "ready"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.SetFulfilment(ctx, user.ID, o2.ID, "completed"); err != nil {
		t.Fatal(err)
	}
}

func TestGraceFloorDoesNotBlockOrders(t *testing.T) {
	app, user, batch := fixture(t)
	ctx := context.Background()
	admin := mustAdmin(t, app)
	acc, err := app.Q.GetAccountByOwner(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.AdjustWallet(ctx, admin, acc.ID, -10000, "tarik ke lantai uji"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.CreateBatch(ctx, user.ID, batch.BrandID, BatchIn{
		Title: "baru", OpensAt: time.Now(), ClosesAt: time.Now().Add(time.Hour), ReadyAt: time.Now().Add(2 * time.Hour),
		AllowPickup: true, Publish: true, Items: []BatchItemIn{{ProductID: firstProduct(t, app, batch.BrandID), Price: 10000}},
	}); err == nil {
		t.Fatal("new batch should be soft-locked")
	}
	o := mustOrder(t, app, batch.Code, "Masih bisa", "081255556666")
	if _, err := app.MarkPaid(ctx, user.ID, o.ID); err != nil {
		t.Fatal(err)
	}
	w := walletOf(t, app, user.ID)
	if w.BalanceKoin != 5000-10000-100 {
		t.Fatalf("balance %d", w.BalanceKoin)
	}
}

func TestTopupApprovalOnce(t *testing.T) {
	app, user, _ := fixture(t)
	ctx := context.Background()
	top, err := app.CreateTopup(ctx, user.ID, 50000, "idem-1")
	if err != nil {
		t.Fatal(err)
	}
	again, err := app.CreateTopup(ctx, user.ID, 50000, "idem-1")
	if err != nil || again.ID != top.ID {
		t.Fatal("idempotency", err)
	}
	if top.AdminFeeRp != 1000 {
		t.Fatalf("fee %d", top.AdminFeeRp)
	}
	if top.UniqueSuffix%100 == 0 {
		t.Fatal("round suffix")
	}
	if top.TotalDueRp != top.AmountRp+top.AdminFeeRp+int64(top.UniqueSuffix) {
		t.Fatalf("total %d", top.TotalDueRp)
	}
	if top.KoinToCredit != top.AmountRp+int64(top.UniqueSuffix) {
		t.Fatalf("credit snapshot %d", top.KoinToCredit)
	}
	admin := mustAdmin(t, app)
	if _, err := app.PublishFee(ctx, admin, FeeIn{FixedRp: 2500, Note: "naik"}); err != nil {
		t.Fatal(err)
	}
	fresh, err := app.Q.GetTopup(ctx, top.ID)
	if err != nil || fresh.AdminFeeRp != 1000 {
		t.Fatalf("fee changed under an open top-up: %+v %v", fresh.AdminFeeRp, err)
	}
	newer, err := app.CreateTopup(ctx, user.ID, 25000, "")
	if err != nil || newer.AdminFeeRp != 2500 {
		t.Fatalf("new fee %d %v", newer.AdminFeeRp, err)
	}
	if _, err := app.SubmitTopupProof(ctx, user.ID, top.ID, bytes.NewReader([]byte("bukti")), "image/jpeg", 5); err != nil {
		t.Fatal(err)
	}
	approved, err := app.ApproveTopup(ctx, admin, top.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.ApproveTopup(ctx, admin, top.ID, nil); err == nil {
		t.Fatal("second approve should fail")
	}
	w := walletOf(t, app, user.ID)
	// 5000 signup + credit, and the other topup is not approved.
	if w.BalanceKoin != 5000+approved.KoinToCredit {
		t.Fatalf("balance %d want %d", w.BalanceKoin, 5000+approved.KoinToCredit)
	}
	if n := countLedger(t, app, w.ID, "topup_credit"); n != 1 {
		t.Fatalf("credits %d", n)
	}
	if _, err := app.SubmitTopupProof(ctx, user.ID, newer.ID, bytes.NewReader([]byte("x")), "image/jpeg", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := app.RejectTopup(ctx, admin, newer.ID, "Bukti tidak terbaca"); err != nil {
		t.Fatal(err)
	}
	w = walletOf(t, app, user.ID)
	if w.BalanceKoin != 5000+approved.KoinToCredit || countLedger(t, app, w.ID, "topup_credit") != 1 {
		t.Fatal("reject credited koin")
	}
}

func TestParallelOrdersStayUnique(t *testing.T) {
	app, _, batch := fixture(t)
	const n = 8
	itemID := firstItem(t, app, batch.ID)
	var wg sync.WaitGroup
	errs := make([]error, n)
	ids := make([]uuid.UUID, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			o, err := app.PlaceOrder(context.Background(), batch.Code, PlaceOrder{
				Name: fmt.Sprintf("P%d", i), WhatsApp: fmt.Sprintf("081300000%03d", i), Method: "pickup",
				Items: []CartItem{{BatchItemID: itemID, Qty: 1}},
			})
			errs[i] = err
			ids[i] = o.ID
		}(i)
	}
	wg.Wait()
	seen := map[int64]struct{}{}
	for i, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
		o, err := app.Q.GetOrder(context.Background(), ids[i])
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := seen[o.TotalDue]; ok {
			t.Fatal("collision", o.TotalDue)
		}
		seen[o.TotalDue] = struct{}{}
	}
}

func fixture(t *testing.T) (*App, db.User, db.PoBatch) {
	t.Helper()
	reset(t)
	pool, err := pgxpool.New(context.Background(), os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	cfg := config.Config{DevLogin: true, AdminEmail: "admin@pesenin.test", PublicOrigin: "http://127.0.0.1:43123", SessionTTL: time.Hour, KYCGate: "off"}
	app := New(pool, cfg, storage.Local{Dir: t.TempDir()}, nil)
	ctx := context.Background()
	email := "rina-" + uuid.NewString()[:8] + "@test.local"
	user, err := app.DevLogin(ctx, email, "Rini")
	if err != nil {
		t.Fatal(err)
	}
	brand, _, err := app.CreateBrand(ctx, user.ID, NewBrand{Name: "Dapur Uji", Slug: "uji-" + uuid.NewString()[:8]})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.AddBank(ctx, user.ID, brand.ID, BankIn{BankCode: "BCA", AccountNumber: "123", HolderName: "Rini", IsDefault: true}); err != nil {
		t.Fatal(err)
	}
	p, err := app.SaveProduct(ctx, user.ID, brand.ID, nil, ProductIn{Name: "Roti", Price: 15000})
	if err != nil {
		t.Fatal(err)
	}
	q := int32(100)
	batch, err := app.CreateBatch(ctx, user.ID, brand.ID, BatchIn{
		Title: "PO uji", OpensAt: time.Now().Add(-time.Hour), ClosesAt: time.Now().Add(24 * time.Hour),
		ReadyAt: time.Now().Add(48 * time.Hour), AllowPickup: true, AllowDelivery: true, Publish: true,
		QuotaTotal: &q, Items: []BatchItemIn{{ProductID: p.ID, Price: 15000, Quota: &q}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return app, user, batch
}

func mustOrder(t *testing.T, app *App, code, name, wa string) db.Order {
	t.Helper()
	o, err := app.PlaceOrder(context.Background(), code, PlaceOrder{
		Name: name, WhatsApp: wa, Method: "pickup",
		Items: []CartItem{{BatchItemID: firstItem(t, app, mustBatchID(t, app, code)), Qty: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return o
}

func mustBatchID(t *testing.T, app *App, code string) uuid.UUID {
	t.Helper()
	b, err := app.Q.GetBatchByCode(context.Background(), code)
	if err != nil {
		t.Fatal(err)
	}
	return b.ID
}

func firstItem(t *testing.T, app *App, batchID uuid.UUID) uuid.UUID {
	t.Helper()
	items, err := app.Q.ListBatchItems(context.Background(), batchID)
	if err != nil || len(items) == 0 {
		t.Fatal(err)
	}
	return items[0].ID
}

func firstProduct(t *testing.T, app *App, brandID uuid.UUID) uuid.UUID {
	t.Helper()
	rows, err := app.Q.ListProducts(context.Background(), brandID)
	if err != nil || len(rows) == 0 {
		t.Fatal(err)
	}
	return rows[0].ID
}

func mustID(t *testing.T, app *App, total int64) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := app.Pool.QueryRow(context.Background(), `SELECT id FROM orders WHERE total_due = $1 LIMIT 1`, total).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func walletOf(t *testing.T, app *App, userID uuid.UUID) db.Wallet {
	t.Helper()
	acc, err := app.Q.GetAccountByOwner(context.Background(), userID)
	if err != nil {
		t.Fatal(err)
	}
	w, err := app.Q.GetWalletByAccount(context.Background(), acc.ID)
	if err != nil {
		t.Fatal(err)
	}
	sum, err := app.Q.SumWalletTx(context.Background(), w.ID)
	if err != nil {
		t.Fatal(err)
	}
	if sum != w.BalanceKoin {
		t.Fatalf("ledger %d != cache %d", sum, w.BalanceKoin)
	}
	return w
}

func countLedger(t *testing.T, app *App, walletID uuid.UUID, typ string) int {
	t.Helper()
	var n int
	err := app.Pool.QueryRow(context.Background(), `SELECT count(*) FROM wallet_transactions WHERE wallet_id = $1 AND type = $2`, walletID, typ).Scan(&n)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func mustAdmin(t *testing.T, app *App) db.User {
	t.Helper()
	u, err := app.DevLogin(context.Background(), "admin@pesenin.test", "Admin")
	if err != nil {
		t.Fatal(err)
	}
	if !u.IsPlatformAdmin {
		t.Fatal("admin flag")
	}
	return u
}

func reset(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	_, err = conn.Exec(ctx, `
		TRUNCATE brand_kyc_documents, brand_kyc_events, whatsapp_otps, auth_tokens,
			audit_logs, notifications, push_subscriptions, message_templates,
			payment_proofs, order_charges, order_items, orders, customers,
			batch_items, po_batches, extra_charges, product_variants, products,
			bank_accounts, stores, brands, wallet_transactions, wallets,
			account_members, topups, sessions, auth_identities, accounts, users
		RESTART IDENTITY CASCADE;
		DELETE FROM admin_fee_configs;
		INSERT INTO admin_fee_configs (channel, fixed_rp, percent_bps, min_fee_rp, note)
		VALUES ('manual_transfer', 1000, 0, 0, 'test default')`)
	if err != nil {
		t.Fatal(err)
	}
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
