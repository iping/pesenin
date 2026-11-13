package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"pesenin.product.biz.id/api/internal/db"
	"pesenin.product.biz.id/api/internal/images"
	"pesenin.product.biz.id/api/internal/koin"
	"pesenin.product.biz.id/api/internal/uniqueamount"
)

type TopupQuote struct {
	Amount   int64
	Fee      int64
	FeeID    uuid.UUID
	Fixed    int64
	Percent  int32
	Min      int64
	Max      *int64
	KoinHint string
}

func (a *App) QuoteTopup(ctx context.Context, amount int64) (TopupQuote, Settings, error) {
	s, err := a.LoadSettings(ctx)
	if err != nil {
		return TopupQuote{}, s, err
	}
	if !koin.ValidCustomAmount(amount, s.CustomMin, s.CustomMax, s.CustomMultiple) {
		return TopupQuote{}, s, E(400, "bad_amount", "Minimal Rp 25.000, maksimal Rp 1.000.000, kelipatan Rp 1.000.")
	}
	cfg, err := a.Q.GetActiveFeeConfig(ctx, "manual_transfer")
	if err != nil {
		return TopupQuote{}, s, err
	}
	fee := koin.Fee(amount, cfg.FixedRp, int(cfg.PercentBps), cfg.MinFeeRp, cfg.MaxFeeRp)
	return TopupQuote{
		Amount: amount, Fee: fee, FeeID: cfg.ID, Fixed: cfg.FixedRp, Percent: cfg.PercentBps, Min: cfg.MinFeeRp, Max: cfg.MaxFeeRp,
	}, s, nil
}

func (a *App) CreateTopup(ctx context.Context, userID uuid.UUID, amount int64, idem string) (db.Topup, error) {
	if idem != "" {
		if existing, err := a.Q.GetTopupByIdempotency(ctx, &idem); err == nil {
			return existing, nil
		}
	}
	acc, err := a.Q.GetAccountByOwner(ctx, userID)
	if err != nil {
		return db.Topup{}, E(403, "no_account", "Akun penjual belum ada.")
	}
	quote, settings, err := a.QuoteTopup(ctx, amount)
	if err != nil {
		return db.Topup{}, err
	}
	bank, err := a.Q.GetActivePlatformBank(ctx)
	if err != nil {
		return db.Topup{}, E(500, "no_platform_bank", "Rekening Pesenin belum diset.")
	}
	var created db.Topup
	err = a.txConn(ctx, func(q *db.Queries, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", lockKey(bank.ID)^0x70); err != nil {
			return err
		}
		reserved, err := q.ListReservedTopupTotals(ctx, bank.ID)
		if err != nil {
			return err
		}
		taken := map[int64]struct{}{}
		for _, t := range reserved {
			taken[t] = struct{}{}
		}
		hours := settings.TopupExpiryHours
		if hours <= 0 {
			hours = 24
		}
		exp := time.Now().Add(time.Duration(hours) * time.Hour)
		var key *string
		if idem != "" {
			key = &idem
		}
		for try := 0; try < 5; try++ {
			suffix, total, err := uniqueamount.Pick(quote.Amount+quote.Fee, taken)
			if err != nil {
				return E(409, "no_suffix", "Kode unik top up lagi penuh. Coba lagi sebentar.")
			}
			if _, err := tx.Exec(ctx, "SAVEPOINT ins_topup"); err != nil {
				return err
			}
			code, err := q.NextTopupCode(ctx)
			if err != nil {
				return err
			}
			// Koin credited on approval = amount + suffix. Fee stays with us.
			credit := koin.Credit(quote.Amount, int64(suffix), quote.Fee, nil)
			row, err := q.InsertTopup(ctx, db.InsertTopupParams{
				AccountID: acc.ID, Code: code, AmountRp: quote.Amount, UniqueSuffix: suffix,
				AdminFeeRp: quote.Fee, TotalDueRp: total, KoinToCredit: credit,
				FeeConfigID: quote.FeeID, FeeFixedRp: quote.Fixed, FeePercentBps: quote.Percent,
				PlatformBankAccountID: bank.ID, ExpiresAt: exp, IdempotencyKey: key,
			})
			if err == nil {
				if _, err := tx.Exec(ctx, "RELEASE SAVEPOINT ins_topup"); err != nil {
					return err
				}
				created = row
				return nil
			}
			if _, rbErr := tx.Exec(ctx, "ROLLBACK TO SAVEPOINT ins_topup"); rbErr != nil {
				return rbErr
			}
			if isUnique(err, "ux_topups_active_amount") {
				taken[total] = struct{}{}
				continue
			}
			if isUnique(err, "topups_idempotency_key_key") {
				return errIdempotent
			}
			return err
		}
		return E(409, "no_suffix", "Kode unik top up lagi penuh.")
	})
	if errors.Is(err, errIdempotent) && idem != "" {
		return a.Q.GetTopupByIdempotency(ctx, &idem)
	}
	return created, err
}

func (a *App) SubmitTopupProof(ctx context.Context, userID, topupID uuid.UUID, r io.Reader, mime string) (db.Topup, error) {
	res, err := prepareImage(r, mime, images.Proof)
	if err != nil {
		return db.Topup{}, err
	}
	dir := "topups/" + uuid.NewString()
	acc, err := a.Q.GetAccountByOwner(ctx, userID)
	if err != nil {
		return db.Topup{}, err
	}
	var out db.Topup
	err = a.tx(ctx, func(q *db.Queries) error {
		t, err := q.GetTopupForUpdate(ctx, topupID)
		if err != nil {
			return err
		}
		if t.AccountID != acc.ID {
			return E(403, "forbidden", "Bukan top up kamu.")
		}
		if t.Status != "pending" && t.Status != "awaiting_verification" {
			return E(409, "bad_state", "Top up ini sudah tidak menerima bukti.")
		}
		key, n, err := a.putImage(ctx, dir, res)
		if err != nil {
			return err
		}
		if _, err := q.InsertProof(ctx, db.InsertProofParams{TopupID: pgu(t.ID), ObjectKey: key, Mime: res.MIME, SizeBytes: int32(n)}); err != nil {
			return err
		}
		out, err = q.SetTopupProof(ctx, db.SetTopupProofParams{ID: t.ID, ProofKey: &key})
		return err
	})
	if err == nil {
		admins := a.adminUsers(ctx)
		for _, adm := range admins {
			id := adm.ID
			a.Notify(ctx, Note{
				UserID: id, Event: "topup_submitted", Title: "Top up menunggu",
				Body: "Ping! Ada bukti top up yang perlu dicek.",
				Link: "/app/admin", Dedupe: "topup_submitted:" + out.ID.String(), Push: false,
			})
		}
	}
	return out, err
}

func (a *App) ApproveTopup(ctx context.Context, admin db.User, topupID uuid.UUID, received *int64) (db.Topup, error) {
	if err := a.requireAdmin(admin); err != nil {
		return db.Topup{}, err
	}
	var out db.Topup
	err := a.tx(ctx, func(q *db.Queries) error {
		t, err := q.GetTopupForUpdate(ctx, topupID)
		if err != nil {
			return err
		}
		if t.Status != "awaiting_verification" && t.Status != "expired" {
			return E(409, "bad_state", "Top up ini tidak bisa disetujui.")
		}
		credit := koin.Credit(t.AmountRp, int64(t.UniqueSuffix), t.AdminFeeRp, received)
		w, err := q.GetWalletForAccountUpdate(ctx, t.AccountID)
		if err != nil {
			return err
		}
		if _, _, err := applyLedger(ctx, q, w.ID, ledgerIn{
			Type: "topup_credit", Amount: credit, Key: "topup_credit:" + t.ID.String(),
			TopupID: &t.ID, Actor: &admin.ID, Reason: "Top up " + t.Code,
		}); err != nil {
			return err
		}
		now := time.Now()
		out, err = q.SetTopupStatus(ctx, db.SetTopupStatusParams{
			ID: t.ID, Status: "approved", ReceivedAmountRp: received, ReviewedBy: pgu(admin.ID),
			ReviewedAt: pgt(now), KoinToCredit: credit,
		})
		if err != nil {
			return err
		}
		a.audit(ctx, q, &t.AccountID, &admin.ID, "topup.approve", "topup", t.ID.String(),
			map[string]any{"status": t.Status, "expected": t.TotalDueRp},
			map[string]any{"status": "approved", "credit": credit, "received": received})
		return nil
	})
	if err == nil {
		acc, e := a.Q.GetAccountByID(ctx, out.AccountID)
		if e == nil {
			a.Notify(ctx, Note{
				UserID: acc.OwnerUserID, Account: &acc.ID, Event: "topup_approved", Title: "Top up disetujui",
				Body: "Ping! Top up kamu masuk. Saldo koin sudah bertambah.",
				Link: "/app/koin", Dedupe: "topup_approved:" + out.ID.String(), Push: false,
			})
		}
	}
	return out, err
}

func (a *App) RejectTopup(ctx context.Context, admin db.User, topupID uuid.UUID, reason string) (db.Topup, error) {
	if err := a.requireAdmin(admin); err != nil {
		return db.Topup{}, err
	}
	if reason == "" {
		return db.Topup{}, E(400, "need_reason", "Alasan penolakan wajib diisi.")
	}
	var out db.Topup
	err := a.tx(ctx, func(q *db.Queries) error {
		t, err := q.GetTopupForUpdate(ctx, topupID)
		if err != nil {
			return err
		}
		if t.Status != "awaiting_verification" && t.Status != "pending" {
			return E(409, "bad_state", "Top up ini tidak bisa ditolak.")
		}
		now := time.Now()
		out, err = q.SetTopupStatus(ctx, db.SetTopupStatusParams{
			ID: t.ID, Status: "rejected", ReviewedBy: pgu(admin.ID), ReviewedAt: pgt(now),
			RejectReason: &reason, KoinToCredit: t.KoinToCredit,
		})
		if err != nil {
			return err
		}
		a.audit(ctx, q, &t.AccountID, &admin.ID, "topup.reject", "topup", t.ID.String(),
			map[string]string{"status": t.Status}, map[string]string{"status": "rejected", "reason": reason})
		return nil
	})
	if err == nil {
		acc, e := a.Q.GetAccountByID(ctx, out.AccountID)
		if e == nil {
			a.Notify(ctx, Note{
				UserID: acc.OwnerUserID, Account: &acc.ID, Event: "topup_rejected", Title: "Top up ditolak",
				Body: "Top up ditolak: " + reason + ". Kamu bisa coba lagi.",
				Link: "/app/koin", Dedupe: "topup_rejected:" + out.ID.String(), Push: false,
			})
		}
	}
	return out, err
}

func (a *App) AdjustWallet(ctx context.Context, admin db.User, accountID uuid.UUID, amount int64, reason string) (int64, error) {
	if err := a.requireAdmin(admin); err != nil {
		return 0, err
	}
	if reason == "" || amount == 0 {
		return 0, E(400, "bad_adjust", "Isi nominal dan alasan penyesuaian.")
	}
	var bal int64
	err := a.tx(ctx, func(q *db.Queries) error {
		w, err := q.GetWalletForAccountUpdate(ctx, accountID)
		if err != nil {
			return err
		}
		bal, _, err = applyLedger(ctx, q, w.ID, ledgerIn{
			Type: "adjustment", Amount: amount, Key: "adjustment:" + uuid.NewString(),
			Actor: &admin.ID, Reason: reason,
		})
		if err != nil {
			return err
		}
		a.audit(ctx, q, &accountID, &admin.ID, "wallet.adjust", "wallet", w.ID.String(),
			map[string]int64{"balance": w.BalanceKoin}, map[string]any{"balance": bal, "amount": amount, "reason": reason})
		return nil
	})
	return bal, err
}

type FeeIn struct {
	FixedRp    int64
	PercentBps int32
	MinFee     int64
	MaxFee     *int64
	From       time.Time
	Note       string
}

func (a *App) PublishFee(ctx context.Context, admin db.User, in FeeIn) (db.AdminFeeConfig, error) {
	if err := a.requireAdmin(admin); err != nil {
		return db.AdminFeeConfig{}, err
	}
	if in.FixedRp < 0 || in.PercentBps < 0 {
		return db.AdminFeeConfig{}, E(400, "bad_fee", "Biaya admin tidak boleh minus.")
	}
	if in.From.IsZero() {
		in.From = time.Now()
	}
	row, err := a.Q.InsertFeeConfig(ctx, db.InsertFeeConfigParams{
		Channel: "manual_transfer", FixedRp: in.FixedRp, PercentBps: in.PercentBps, MinFeeRp: in.MinFee,
		MaxFeeRp: in.MaxFee, EffectiveFrom: in.From, CreatedBy: pgu(admin.ID), Note: in.Note,
	})
	if err != nil {
		return row, err
	}
	a.audit(ctx, a.Q, nil, &admin.ID, "fee.publish", "admin_fee_config", row.ID.String(), nil, in)
	return row, nil
}

func (a *App) SaveSettings(ctx context.Context, admin db.User, values map[string]any) error {
	if err := a.requireAdmin(admin); err != nil {
		return err
	}
	allowed := map[string]bool{
		"topup_presets": true, "custom_min": true, "custom_max": true, "custom_multiple": true,
		"topup_expiry_hours": true, "order_fee_koin": true, "signup_free_koin": true,
		"grace_floor_koin": true, "low_balance_koin": true, "payment_deadline_hours": true,
	}
	for k, v := range values {
		if !allowed[k] {
			return E(400, "bad_setting", "Pengaturan "+k+" tidak dikenal.")
		}
		raw, err := jsonMarshal(v)
		if err != nil {
			return err
		}
		if _, err := a.Q.UpsertSetting(ctx, db.UpsertSettingParams{Key: k, Value: raw, UpdatedBy: pgu(admin.ID)}); err != nil {
			return err
		}
		a.audit(ctx, a.Q, nil, &admin.ID, "settings.update", "platform_settings", k, nil, v)
	}
	return nil
}

type Report struct {
	TopupCount         int32
	TopupRp            int64
	FeeRp              int64
	KoinPaid           int64
	KoinFree           int64
	KoinAdjust         int64
	KoinUsed           int64
	KoinOutstanding    int64
	AccountsBelowFloor int32
}

func (a *App) KoinReport(ctx context.Context, admin db.User, from, to time.Time) (Report, error) {
	if err := a.requireAdmin(admin); err != nil {
		return Report{}, err
	}
	sums, err := a.Q.SumApprovedTopups(ctx, db.SumApprovedTopupsParams{ReviewedAt: pgt(from), ReviewedAt_2: pgt(to)})
	if err != nil {
		return Report{}, err
	}
	paid, err := a.Q.SumLedgerByType(ctx, db.SumLedgerByTypeParams{Type: "topup_credit", CreatedAt: from, CreatedAt_2: to})
	if err != nil {
		return Report{}, err
	}
	free, err := a.Q.SumLedgerByType(ctx, db.SumLedgerByTypeParams{Type: "promo_free_credit", CreatedAt: from, CreatedAt_2: to})
	if err != nil {
		return Report{}, err
	}
	adj, err := a.Q.SumLedgerByType(ctx, db.SumLedgerByTypeParams{Type: "adjustment", CreatedAt: from, CreatedAt_2: to})
	if err != nil {
		return Report{}, err
	}
	used, err := a.Q.SumLedgerByType(ctx, db.SumLedgerByTypeParams{Type: "order_debit", CreatedAt: from, CreatedAt_2: to})
	if err != nil {
		return Report{}, err
	}
	out, err := a.Q.SumOutstandingKoin(ctx)
	if err != nil {
		return Report{}, err
	}
	settings, _ := a.LoadSettings(ctx)
	below, err := a.Q.CountWalletsAtOrBelow(ctx, settings.GraceFloor)
	if err != nil {
		return Report{}, err
	}
	if used < 0 {
		used = -used
	}
	return Report{
		TopupCount: sums.TopupCount, TopupRp: sums.TopupRp, FeeRp: sums.FeeRp,
		KoinPaid: paid, KoinFree: free, KoinAdjust: adj, KoinUsed: used,
		KoinOutstanding: out, AccountsBelowFloor: below,
	}, nil
}

func (a *App) adminUsers(ctx context.Context) []db.User {
	rows, err := a.Pool.Query(ctx, `SELECT id, email, name, avatar_url, google_sub, is_platform_admin, created_at FROM users WHERE is_platform_admin`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []db.User
	for rows.Next() {
		var u db.User
		if err := rows.Scan(&u.ID, &u.Email, &u.Name, &u.AvatarUrl, &u.GoogleSub, &u.IsPlatformAdmin, &u.CreatedAt); err == nil {
			out = append(out, u)
		}
	}
	return out
}

func jsonMarshal(v any) ([]byte, error) {
	return json.Marshal(v)
}
