package httpapi

import (
	"encoding/json"
	"net/http"
	"time"

	"io"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"pesenin.product.biz.id/api/internal/db"
	"pesenin.product.biz.id/api/internal/service"
)

func (s *Server) wallet(w http.ResponseWriter, r *http.Request) {
	me, err := s.App.LoadMe(r.Context(), user(r))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{
		"wallet": meJSON(me, nil)["wallet"], "softLocked": me.Locked, "lowBalance": me.Low,
		"graceFloor": me.Floor, "orderFee": me.Fee,
	})
}

func (s *Server) ledger(w http.ResponseWriter, r *http.Request) {
	acc, err := s.App.Q.GetAccountByOwner(r.Context(), user(r).ID)
	if err != nil {
		writeErr(w, err)
		return
	}
	wal, err := s.App.Q.GetWalletByAccount(r.Context(), acc.ID)
	if err != nil {
		writeErr(w, err)
		return
	}
	rows, err := s.App.Q.ListWalletTx(r.Context(), db.ListWalletTxParams{WalletID: wal.ID, Limit: 50})
	if err != nil {
		writeErr(w, err)
		return
	}
	out := []any{}
	for _, t := range rows {
		out = append(out, map[string]any{
			"id": t.ID, "type": t.Type, "amountKoin": t.AmountKoin, "balanceAfter": t.BalanceAfter,
			"reason": t.Reason, "createdAt": ts(t.CreatedAt), "orderId": uuidOrNil(t.OrderID), "topupId": uuidOrNil(t.TopupID),
		})
	}
	writeJSON(w, 200, out)
}

func (s *Server) topupOptions(w http.ResponseWriter, r *http.Request) {
	settings, err := s.App.LoadSettings(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	fee, err := s.App.Q.GetActiveFeeConfig(r.Context(), "manual_transfer")
	if err != nil {
		writeErr(w, err)
		return
	}
	bank, _ := s.App.Q.GetActivePlatformBank(r.Context())
	writeJSON(w, 200, map[string]any{
		"presets": settings.TopupPresets, "customMin": settings.CustomMin, "customMax": settings.CustomMax,
		"customMultiple": settings.CustomMultiple, "expiryHours": settings.TopupExpiryHours,
		"fee":  map[string]any{"fixedRp": fee.FixedRp, "percentBps": fee.PercentBps, "id": fee.ID},
		"bank": map[string]any{"bankCode": bank.BankCode, "accountNumber": bank.AccountNumber, "holderName": bank.HolderName},
	})
}

func (s *Server) topupQuote(w http.ResponseWriter, r *http.Request) {
	var raw struct {
		Amount int64 `json:"amount"`
	}
	if err := readJSON(r, &raw); err != nil {
		writeErr(w, err)
		return
	}
	q, _, err := s.App.QuoteTopup(r.Context(), raw.Amount)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{
		"amount": q.Amount, "adminFee": q.Fee, "note": "Kode unik 3 digit ditambah saat kamu konfirmasi.",
	})
}

func (s *Server) listTopups(w http.ResponseWriter, r *http.Request) {
	acc, err := s.App.Q.GetAccountByOwner(r.Context(), user(r).ID)
	if err != nil {
		writeErr(w, err)
		return
	}
	rows, err := s.App.Q.ListTopupsByAccount(r.Context(), acc.ID)
	if err != nil {
		writeErr(w, err)
		return
	}
	bank, _ := s.App.Q.GetActivePlatformBank(r.Context())
	out := []any{}
	for _, t := range rows {
		out = append(out, topupJSON(t, &bank))
	}
	writeJSON(w, 200, out)
}

func (s *Server) createTopup(w http.ResponseWriter, r *http.Request) {
	var raw struct {
		Amount int64 `json:"amount"`
	}
	if err := readJSON(r, &raw); err != nil {
		writeErr(w, err)
		return
	}
	idem := r.Header.Get("Idempotency-Key")
	t, err := s.App.CreateTopup(r.Context(), user(r).ID, raw.Amount, idem)
	if err != nil {
		writeErr(w, err)
		return
	}
	bank, _ := s.App.Q.GetActivePlatformBank(r.Context())
	writeJSON(w, 201, topupJSON(t, &bank))
}

func (s *Server) getTopup(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "topupId")
	if err != nil {
		writeErr(w, err)
		return
	}
	t, err := s.App.Q.GetTopup(r.Context(), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	acc, err := s.App.Q.GetAccountByOwner(r.Context(), user(r).ID)
	if err != nil || (t.AccountID != acc.ID && !user(r).IsPlatformAdmin) {
		writeErr(w, service.E(403, "forbidden", "Bukan top up kamu."))
		return
	}
	bank, _ := s.App.Q.GetActivePlatformBank(r.Context())
	writeJSON(w, 200, topupJSON(t, &bank))
}

func (s *Server) topupProof(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "topupId")
	if err != nil {
		writeErr(w, err)
		return
	}
	s.uploadProof(w, r, func(rc io.Reader, mime string) error {
		_, err := s.App.SubmitTopupProof(r.Context(), user(r).ID, id, rc, mime)
		return err
	})
}

func (s *Server) notifications(w http.ResponseWriter, r *http.Request) {
	rows, err := s.App.Q.ListNotifications(r.Context(), user(r).ID)
	if err != nil {
		writeErr(w, err)
		return
	}
	out := []any{}
	for _, n := range rows {
		out = append(out, map[string]any{
			"id": n.ID, "event": n.EventType, "title": n.Title, "body": n.Body,
			"link": n.DeepLink, "readAt": tsP(n.ReadAt), "createdAt": ts(n.CreatedAt),
		})
	}
	writeJSON(w, 200, out)
}

func (s *Server) unread(w http.ResponseWriter, r *http.Request) {
	n, err := s.App.Q.UnreadCount(r.Context(), user(r).ID)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]int32{"count": n})
}

func (s *Server) markRead(w http.ResponseWriter, r *http.Request) {
	var raw struct {
		ID *uuid.UUID `json:"id"`
	}
	_ = readJSON(r, &raw)
	id := pgtype.UUID{}
	if raw.ID != nil {
		id = pgtype.UUID{Bytes: [16]byte(*raw.ID), Valid: true}
	}
	if err := s.App.Q.MarkNotificationsRead(r.Context(), db.MarkNotificationsReadParams{RecipientUserID: user(r).ID, ID: id}); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) savePush(w http.ResponseWriter, r *http.Request) {
	var raw struct {
		Endpoint string `json:"endpoint"`
		Keys     struct {
			P256dh string `json:"p256dh"`
			Auth   string `json:"auth"`
		} `json:"keys"`
	}
	if err := readJSON(r, &raw); err != nil {
		writeErr(w, err)
		return
	}
	ua := r.UserAgent()
	row, err := s.App.Q.UpsertPushSub(r.Context(), db.UpsertPushSubParams{
		UserID: user(r).ID, Endpoint: raw.Endpoint, P256dh: raw.Keys.P256dh, AuthKey: raw.Keys.Auth, UserAgent: &ua,
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 201, map[string]any{"id": row.ID})
}

func (s *Server) deletePush(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "id")
	if err != nil {
		writeErr(w, err)
		return
	}
	if err := s.App.Q.DeletePushSub(r.Context(), db.DeletePushSubParams{ID: id, UserID: user(r).ID}); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) adminTopups(w http.ResponseWriter, r *http.Request) {
	if err := s.mustAdmin(w, r); err != nil {
		return
	}
	var st *string
	if q := r.URL.Query().Get("status"); q != "" {
		st = &q
	}
	rows, err := s.App.Q.ListTopupsByStatus(r.Context(), st)
	if err != nil {
		writeErr(w, err)
		return
	}
	bank, _ := s.App.Q.GetActivePlatformBank(r.Context())
	out := []any{}
	for _, t := range rows {
		out = append(out, topupJSON(t, &bank))
	}
	writeJSON(w, 200, out)
}

func (s *Server) adminApprove(w http.ResponseWriter, r *http.Request) {
	if err := s.mustAdmin(w, r); err != nil {
		return
	}
	id, err := pathUUID(r, "topupId")
	if err != nil {
		writeErr(w, err)
		return
	}
	var raw struct {
		ReceivedAmount *int64 `json:"receivedAmountRp"`
	}
	_ = readJSON(r, &raw)
	t, err := s.App.ApproveTopup(r.Context(), user(r), id, raw.ReceivedAmount)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, topupJSON(t, nil))
}

func (s *Server) adminReject(w http.ResponseWriter, r *http.Request) {
	if err := s.mustAdmin(w, r); err != nil {
		return
	}
	id, err := pathUUID(r, "topupId")
	if err != nil {
		writeErr(w, err)
		return
	}
	var raw struct {
		Reason string `json:"reason"`
	}
	if err := readJSON(r, &raw); err != nil {
		writeErr(w, err)
		return
	}
	t, err := s.App.RejectTopup(r.Context(), user(r), id, raw.Reason)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, topupJSON(t, nil))
}

func (s *Server) adminFees(w http.ResponseWriter, r *http.Request) {
	if err := s.mustAdmin(w, r); err != nil {
		return
	}
	rows, err := s.App.Q.ListFeeConfigs(r.Context(), "manual_transfer")
	if err != nil {
		writeErr(w, err)
		return
	}
	active, _ := s.App.Q.GetActiveFeeConfig(r.Context(), "manual_transfer")
	writeJSON(w, 200, map[string]any{"active": active, "versions": rows})
}

func (s *Server) adminPublishFee(w http.ResponseWriter, r *http.Request) {
	if err := s.mustAdmin(w, r); err != nil {
		return
	}
	var raw struct {
		FixedRp    int64  `json:"fixedRp"`
		PercentBps int32  `json:"percentBps"`
		MinFee     int64  `json:"minFeeRp"`
		MaxFee     *int64 `json:"maxFeeRp"`
		Note       string `json:"note"`
	}
	if err := readJSON(r, &raw); err != nil {
		writeErr(w, err)
		return
	}
	row, err := s.App.PublishFee(r.Context(), user(r), service.FeeIn{
		FixedRp: raw.FixedRp, PercentBps: raw.PercentBps, MinFee: raw.MinFee, MaxFee: raw.MaxFee, Note: raw.Note, From: time.Now(),
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 201, row)
}

func (s *Server) adminSettings(w http.ResponseWriter, r *http.Request) {
	if err := s.mustAdmin(w, r); err != nil {
		return
	}
	rows, err := s.App.Q.ListSettings(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	out := map[string]any{}
	for _, row := range rows {
		var v any
		_ = json.Unmarshal(row.Value, &v)
		out[row.Key] = v
	}
	writeJSON(w, 200, out)
}

func (s *Server) adminSaveSettings(w http.ResponseWriter, r *http.Request) {
	if err := s.mustAdmin(w, r); err != nil {
		return
	}
	var raw map[string]any
	if err := readJSON(r, &raw); err != nil {
		writeErr(w, err)
		return
	}
	if err := s.App.SaveSettings(r.Context(), user(r), raw); err != nil {
		writeErr(w, err)
		return
	}
	s.adminSettings(w, r)
}

func (s *Server) adminAdjust(w http.ResponseWriter, r *http.Request) {
	if err := s.mustAdmin(w, r); err != nil {
		return
	}
	id, err := pathUUID(r, "accountId")
	if err != nil {
		writeErr(w, err)
		return
	}
	var raw struct {
		Amount int64  `json:"amountKoin"`
		Reason string `json:"reason"`
	}
	if err := readJSON(r, &raw); err != nil {
		writeErr(w, err)
		return
	}
	bal, err := s.App.AdjustWallet(r.Context(), user(r), id, raw.Amount, raw.Reason)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]int64{"balanceKoin": bal})
}

func (s *Server) adminReport(w http.ResponseWriter, r *http.Request) {
	if err := s.mustAdmin(w, r); err != nil {
		return
	}
	from := time.Now().AddDate(0, 0, -30)
	to := time.Now().Add(time.Hour)
	if v := r.URL.Query().Get("from"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			from = t
		}
	}
	if v := r.URL.Query().Get("to"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			to = t
		}
	}
	rep, err := s.App.KoinReport(r.Context(), user(r), from, to)
	if err != nil {
		writeErr(w, err)
		return
	}
	if r.URL.Query().Get("format") == "csv" {
		w.Header().Set("Content-Type", "text/csv")
		_, _ = w.Write([]byte("topup_count,topup_rp,fee_rp,koin_paid,koin_free,koin_adjust,koin_used,outstanding,below_floor\n"))
		_, _ = w.Write([]byte(itoa(int64(rep.TopupCount)) + "," + itoa(rep.TopupRp) + "," + itoa(rep.FeeRp) + "," + itoa(rep.KoinPaid) + "," + itoa(rep.KoinFree) + "," + itoa(rep.KoinAdjust) + "," + itoa(rep.KoinUsed) + "," + itoa(rep.KoinOutstanding) + "," + itoa(int64(rep.AccountsBelowFloor)) + "\n"))
		return
	}
	writeJSON(w, 200, rep)
}

func (s *Server) adminAudit(w http.ResponseWriter, r *http.Request) {
	if err := s.mustAdmin(w, r); err != nil {
		return
	}
	rows, err := s.App.Q.ListAudit(r.Context(), 100)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, rows)
}

func (s *Server) mustAdmin(w http.ResponseWriter, r *http.Request) error {
	if err := service.AdminSessionAllowed(user(r), via(r)); err != nil {
		writeErr(w, err)
		return err
	}
	return nil
}

func itoa(n int64) string {
	return jsonNumber(n)
}

func jsonNumber(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}
