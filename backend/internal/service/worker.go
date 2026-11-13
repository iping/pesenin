package service

import (
	"context"
	"log/slog"
	"time"

	"pesenin.product.biz.id/api/internal/db"
	"pesenin.product.biz.id/api/internal/orderflow"
)

func (a *App) RunWorker(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	a.Tick(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.Tick(ctx)
		}
	}
}

func (a *App) Tick(ctx context.Context) {
	a.openDue(ctx)
	a.closeDue(ctx)
	a.expireOrders(ctx)
	a.expireTopups(ctx)
	a.flushPushes(ctx)
	a.reconcile(ctx)
}

func (a *App) openDue(ctx context.Context) {
	rows, err := a.Q.ListDueBatchOpens(ctx)
	if err != nil {
		slog.Error("open batches", "err", err)
		return
	}
	for _, b := range rows {
		if _, err := a.Q.SetBatchStatus(ctx, db.SetBatchStatusParams{ID: b.ID, Status: "open"}); err != nil {
			slog.Error("open batch", "err", err, "id", b.ID)
		}
	}
}

func (a *App) closeDue(ctx context.Context) {
	rows, err := a.Q.ListDueBatchCloses(ctx)
	if err != nil {
		slog.Error("close batches", "err", err)
		return
	}
	for _, b := range rows {
		if _, err := a.Q.SetBatchStatus(ctx, db.SetBatchStatusParams{ID: b.ID, Status: "closed"}); err != nil {
			slog.Error("close batch", "err", err, "id", b.ID)
		}
	}
}

func (a *App) expireOrders(ctx context.Context) {
	rows, err := a.Q.ListDueOrders(ctx)
	if err != nil {
		slog.Error("due orders", "err", err)
		return
	}
	for _, row := range rows {
		err := a.tx(ctx, func(q *db.Queries) error {
			o, err := q.GetOrderForUpdate(ctx, row.ID)
			if err != nil {
				return err
			}
			if !orderflow.CanExpire(o.PaymentStatus) {
				return nil
			}
			if err := releaseItems(ctx, q, o.ID); err != nil {
				return err
			}
			_, err = q.SetOrderPayment(ctx, db.SetOrderPaymentParams{ID: o.ID, PaymentStatus: "expired"})
			return err
		})
		if err != nil {
			slog.Error("expire order", "err", err, "id", row.ID)
		}
	}
}

func (a *App) expireTopups(ctx context.Context) {
	rows, err := a.Q.ListDueTopups(ctx)
	if err != nil {
		slog.Error("due topups", "err", err)
		return
	}
	for _, t := range rows {
		if _, err := a.Q.ExpireTopup(ctx, t.ID); err != nil {
			slog.Error("expire topup", "err", err, "id", t.ID)
		}
	}
}

func (a *App) flushPushes(ctx context.Context) {
	if a.Push == nil || !a.Push.Enabled() {
		return
	}
	notes, err := a.Q.ListPendingPushes(ctx)
	if err != nil {
		slog.Error("pending push", "err", err)
		return
	}
	for _, n := range notes {
		subs, err := a.Q.ListPushSubs(ctx, n.RecipientUserID)
		if err != nil {
			continue
		}
		if len(subs) == 0 {
			_ = a.Q.SetNotificationStatus(ctx, db.SetNotificationStatusParams{ID: n.ID, Status: "skipped"})
			continue
		}
		payload := []byte(`{"title":` + jsonString(n.Title) + `,"body":` + jsonString(n.Body) + `,"url":` + jsonString(n.DeepLink) + `}`)
		ok := false
		for _, s := range subs {
			status, err := a.Push.Send(s.Endpoint, s.P256dh, s.AuthKey, payload)
			if status == 404 || status == 410 {
				_ = a.Q.DeletePushByEndpoint(ctx, s.Endpoint)
				continue
			}
			if err != nil || status >= 300 {
				_ = a.Q.BumpPushFailure(ctx, s.Endpoint)
				if s.FailureCount+1 >= 5 {
					_ = a.Q.DeletePushByEndpoint(ctx, s.Endpoint)
				}
				continue
			}
			_ = a.Q.MarkPushSuccess(ctx, s.Endpoint)
			ok = true
		}
		st := "failed"
		if ok {
			st = "sent"
		}
		_ = a.Q.SetNotificationStatus(ctx, db.SetNotificationStatusParams{ID: n.ID, Status: st})
	}
}

func (a *App) reconcile(ctx context.Context) {
	// Once a day-ish: the worker runs every 30s, so only check near midnight minute 0
	// is too fragile. Compare a sample only when the minute is 0 and second < 30 via caller.
	// The integration tests call SumWalletTx directly. Here we log mismatches for every wallet
	// at most by comparing in SQL occasionally.
	if time.Now().Minute()%30 != 0 || time.Now().Second() > 35 {
		return
	}
	rows, err := a.Pool.Query(ctx, `
		SELECT w.id, w.balance_koin, COALESCE(SUM(t.amount_koin), 0)
		FROM wallets w
		LEFT JOIN wallet_transactions t ON t.wallet_id = w.id
		GROUP BY w.id, w.balance_koin
		HAVING w.balance_koin <> COALESCE(SUM(t.amount_koin), 0)
	`)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var cached, sum int64
		if err := rows.Scan(&id, &cached, &sum); err == nil {
			slog.Error("wallet drift", "wallet", id, "cached", cached, "ledger", sum)
		}
	}
}

func jsonString(s string) string {
	b, _ := jsonMarshal(s)
	return string(b)
}
