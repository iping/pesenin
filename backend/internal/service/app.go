package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"pesenin.product.biz.id/api/internal/authlimit"
	"pesenin.product.biz.id/api/internal/config"
	"pesenin.product.biz.id/api/internal/db"
	"pesenin.product.biz.id/api/internal/mailer"
	"pesenin.product.biz.id/api/internal/push"
	"pesenin.product.biz.id/api/internal/storage"
	"pesenin.product.biz.id/api/internal/wasender"
)

// Error is a client-facing failure with a stable code.
type Error struct {
	Code    string
	Message string
	Status  int
}

func (e *Error) Error() string { return e.Message }

func E(status int, code, msg string) *Error {
	return &Error{Status: status, Code: code, Message: msg}
}

// App is the Phase 1 use-case layer. Handlers and tests both call it.
type App struct {
	Pool  *pgxpool.Pool
	Q     *db.Queries
	Cfg   config.Config
	Blob  storage.ObjectStore
	Push  *push.Sender
	Mail  mailer.Sender
	WA    wasender.Sender
	Limit *authlimit.Limiter

	limits *authQuota
	tickMu sync.Mutex
}

func New(pool *pgxpool.Pool, cfg config.Config, blob storage.ObjectStore, sender *push.Sender) *App {
	return &App{
		Pool: pool, Q: db.New(pool), Cfg: cfg, Blob: blob, Push: sender,
		Mail: mailerFrom(cfg), WA: waFrom(cfg), Limit: authlimit.New(),
	}
}

func mailerFrom(cfg config.Config) mailer.Sender {
	if cfg.MailLog() {
		return mailer.Log{}
	}
	port := cfg.SMTPPort
	if port == 0 {
		port = 587
	}
	return mailer.SMTP{
		Host: cfg.SMTPHost, Port: port, Username: cfg.SMTPUser,
		Password: cfg.SMTPPassword, From: cfg.SMTPFrom,
	}
}

func waFrom(cfg config.Config) wasender.Sender {
	switch strings.ToLower(strings.TrimSpace(cfg.WAProvider)) {
	case "", "log":
		return wasender.Log{}
	default:
		return wasender.Provider{
			Name: cfg.WAProvider, APIURL: cfg.WAAPIURL, Token: cfg.WAAPIToken, SenderID: cfg.WASenderID,
		}
	}
}

func (a *App) tx(ctx context.Context, fn func(q *db.Queries) error) error {
	return a.txConn(ctx, func(q *db.Queries, _ pgx.Tx) error { return fn(q) })
}

func (a *App) txConn(ctx context.Context, fn func(q *db.Queries, tx pgx.Tx) error) error {
	tx, err := a.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := fn(db.New(tx), tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

type Settings struct {
	TopupPresets         []int64
	CustomMin            int64
	CustomMax            int64
	CustomMultiple       int64
	TopupExpiryHours     int
	OrderFee             int64
	SignupFree           int64
	GraceFloor           int64
	LowBalance           int64
	PaymentDeadlineHours int
}

func (a *App) LoadSettings(ctx context.Context) (Settings, error) {
	s := Settings{
		TopupPresets:         []int64{25000, 50000, 100000},
		CustomMin:            25000,
		CustomMax:            1_000_000,
		CustomMultiple:       1000,
		TopupExpiryHours:     24,
		OrderFee:             100,
		SignupFree:           5000,
		GraceFloor:           -5000,
		LowBalance:           500,
		PaymentDeadlineHours: 24,
	}
	rows, err := a.Q.ListSettings(ctx)
	if err != nil {
		return s, err
	}
	m := map[string]json.RawMessage{}
	for _, r := range rows {
		m[r.Key] = r.Value
	}
	_ = json.Unmarshal(m["topup_presets"], &s.TopupPresets)
	readInt(m, "custom_min", &s.CustomMin)
	readInt(m, "custom_max", &s.CustomMax)
	readInt(m, "custom_multiple", &s.CustomMultiple)
	readInt(m, "topup_expiry_hours", &s.TopupExpiryHours)
	readInt(m, "order_fee_koin", &s.OrderFee)
	readInt(m, "signup_free_koin", &s.SignupFree)
	readInt(m, "grace_floor_koin", &s.GraceFloor)
	readInt(m, "low_balance_koin", &s.LowBalance)
	readInt(m, "payment_deadline_hours", &s.PaymentDeadlineHours)
	if len(s.TopupPresets) == 0 {
		s.TopupPresets = []int64{25000, 50000, 100000}
	}
	return s, nil
}

func readInt[T int | int64](m map[string]json.RawMessage, key string, dest *T) {
	raw, ok := m[key]
	if !ok {
		return
	}
	var n T
	if json.Unmarshal(raw, &n) == nil {
		*dest = n
	}
}

type ledgerIn struct {
	Type    string
	Amount  int64
	Key     string
	OrderID *uuid.UUID
	TopupID *uuid.UUID
	BrandID *uuid.UUID
	Actor   *uuid.UUID
	Reason  string
}

// applyLedger locks the wallet, inserts one immutable row, and updates the cache.
// A repeated idempotency key returns the existing balance and created=false.
func applyLedger(ctx context.Context, q *db.Queries, walletID uuid.UUID, in ledgerIn) (int64, bool, error) {
	w, err := q.GetWalletForUpdate(ctx, walletID)
	if err != nil {
		return 0, false, err
	}
	existing, err := q.GetWalletTxByKey(ctx, in.Key)
	if err == nil {
		return existing.BalanceAfter, false, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, false, err
	}
	bal := w.BalanceKoin + in.Amount
	_, err = q.InsertWalletTx(ctx, db.InsertWalletTxParams{
		WalletID:       walletID,
		Type:           in.Type,
		AmountKoin:     in.Amount,
		BalanceAfter:   bal,
		IdempotencyKey: in.Key,
		OrderID:        pguPtr(in.OrderID),
		TopupID:        pguPtr(in.TopupID),
		BrandID:        pguPtr(in.BrandID),
		ActorUserID:    pguPtr(in.Actor),
		Reason:         in.Reason,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		existing, err = q.GetWalletTxByKey(ctx, in.Key)
		if err != nil {
			return 0, false, err
		}
		return existing.BalanceAfter, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	if _, err := q.UpdateWalletBalance(ctx, db.UpdateWalletBalanceParams{ID: walletID, BalanceKoin: bal}); err != nil {
		return 0, false, err
	}
	return bal, true, nil
}

func (a *App) audit(ctx context.Context, q *db.Queries, account *uuid.UUID, actor *uuid.UUID, action, entityType, entityID string, before, after any) {
	var b1, b2 []byte
	if before != nil {
		b1, _ = json.Marshal(before)
	}
	if after != nil {
		b2, _ = json.Marshal(after)
	}
	err := q.InsertAudit(ctx, db.InsertAuditParams{
		AccountID:   pguPtr(account),
		ActorUserID: pguPtr(actor),
		Action:      action,
		EntityType:  entityType,
		EntityID:    entityID,
		Before:      b1,
		After:       b2,
	})
	if err != nil {
		slog.Error("audit", "err", err, "action", action)
	}
}

func (a *App) walletByAccount(ctx context.Context, q *db.Queries, accountID uuid.UUID) (db.Wallet, error) {
	return q.GetWalletForAccountUpdate(ctx, accountID)
}

func (a *App) softLocked(ctx context.Context, accountID uuid.UUID) (bool, db.Wallet, Settings, error) {
	s, err := a.LoadSettings(ctx)
	if err != nil {
		return false, db.Wallet{}, s, err
	}
	w, err := a.Q.GetWalletByAccount(ctx, accountID)
	if err != nil {
		return false, w, s, err
	}
	return w.BalanceKoin <= s.GraceFloor, w, s, nil
}

type Note struct {
	UserID  uuid.UUID
	Account *uuid.UUID
	Brand   *uuid.UUID
	Event   string
	Title   string
	Body    string
	Link    string
	Dedupe  string
	Push    bool
}

func (a *App) Notify(ctx context.Context, n Note) {
	status := "skipped"
	if n.Push && a.Push != nil && a.Push.Enabled() {
		status = "pending"
	}
	_, err := a.Q.InsertNotification(ctx, db.InsertNotificationParams{
		RecipientUserID: n.UserID,
		AccountID:       pguPtr(n.Account),
		BrandID:         pguPtr(n.Brand),
		EventType:       n.Event,
		Title:           n.Title,
		Body:            n.Body,
		DeepLink:        n.Link,
		DedupeKey:       n.Dedupe,
		PushEligible:    n.Push,
		Status:          status,
	})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		slog.Error("notify", "err", err, "event", n.Event)
		return
	}
	if status == "pending" {
		go a.flushPushes(context.Background())
	}
}

func (a *App) notifyBrandOwner(ctx context.Context, brandID uuid.UUID, event, title, body, link, dedupe string, push bool) {
	owner, err := a.Q.AccountOwner(ctx, brandID)
	if err != nil {
		slog.Error("notify owner", "err", err)
		return
	}
	brand, err := a.Q.GetBrandByID(ctx, brandID)
	if err != nil {
		return
	}
	acc := brand.AccountID
	a.Notify(ctx, Note{
		UserID: owner.ID, Account: &acc, Brand: &brandID,
		Event: event, Title: title, Body: body, Link: link, Dedupe: dedupe, Push: push,
	})
}

func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func pgu(id uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: [16]byte(id), Valid: true}
}

func pguPtr(id *uuid.UUID) pgtype.UUID {
	if id == nil || *id == uuid.Nil {
		return pgtype.UUID{}
	}
	return pgu(*id)
}

func pgt(t time.Time) pgtype.Timestamptz {
	if t.IsZero() {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: t, Valid: true}
}

func pgtPtr(t *time.Time) pgtype.Timestamptz {
	if t == nil {
		return pgtype.Timestamptz{}
	}
	return pgt(*t)
}

func isUnique(err error, constraint string) bool {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" {
		return constraint == "" || pg.ConstraintName == constraint
	}
	return false
}

func lockKey(id uuid.UUID) int64 {
	var n int64
	for i := 0; i < 8; i++ {
		n = (n << 8) | int64(id[i])
	}
	return n
}

// OwnedBrand checks the signed-in user owns the brand.
func (a *App) OwnedBrand(ctx context.Context, userID, brandID uuid.UUID) (db.Brand, db.Account, error) {
	acc, err := a.Q.GetAccountByOwner(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return db.Brand{}, db.Account{}, E(403, "no_account", "Akun penjual belum dibuat.")
	}
	if err != nil {
		return db.Brand{}, db.Account{}, err
	}
	if acc.Status == "suspended" {
		return db.Brand{}, acc, E(403, "suspended", "Akun ini ditangguhkan.")
	}
	brand, err := a.Q.GetBrandByID(ctx, brandID)
	if errors.Is(err, pgx.ErrNoRows) {
		return db.Brand{}, acc, E(404, "not_found", "Brand tidak ketemu.")
	}
	if err != nil {
		return db.Brand{}, acc, err
	}
	if brand.AccountID != acc.ID {
		return db.Brand{}, acc, E(403, "forbidden", "Ini bukan brand kamu.")
	}
	return brand, acc, nil
}

func (a *App) requireAdmin(user db.User) error {
	if !user.IsPlatformAdmin {
		return E(403, "forbidden", "Khusus admin Pesenin.")
	}
	return nil
}

func jakarta(t time.Time) string {
	loc, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		return t.Format(time.RFC3339)
	}
	tt := t.In(loc)
	days := []string{"Min", "Sen", "Sel", "Rab", "Kam", "Jum", "Sab"}
	return fmt.Sprintf("%s, %02d %s %02d.%02d WIB", days[tt.Weekday()], tt.Day(), tt.Format("Jan"), tt.Hour(), tt.Minute())
}
