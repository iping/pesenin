package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"pesenin.product.biz.id/api/internal/db"
	"pesenin.product.biz.id/api/internal/orderflow"
	"pesenin.product.biz.id/api/internal/text"
	"pesenin.product.biz.id/api/internal/uniqueamount"
)

type CartItem struct {
	BatchItemID uuid.UUID `json:"batchItemId"`
	Qty         int32     `json:"qty"`
}

type PlaceOrder struct {
	Name           string     `json:"name"`
	WhatsApp       string     `json:"whatsapp"`
	Method         string     `json:"method"`
	Address        string     `json:"address"`
	Note           string     `json:"note"`
	Items          []CartItem `json:"items"`
	IdempotencyKey string     `json:"idempotencyKey"`
	SlotIndex      int        `json:"slotIndex"`
}

type OrderDetail struct {
	Order    db.Order
	Items    []db.OrderItem
	Charges  []db.OrderCharge
	Customer db.Customer
	Bank     db.BankAccount
	Proofs   []db.PaymentProof
	Batch    db.PoBatch
	Store    db.Store
	Brand    db.Brand
}

type Quote struct {
	Subtotal int64
	Charges  []db.ExtraCharge
	Extra    int64
	Base     int64
	Lines    []CartItem
}

func applicableCharges(charges []db.ExtraCharge, method string) []db.ExtraCharge {
	var out []db.ExtraCharge
	for _, c := range charges {
		for _, m := range c.FulfilmentMethods {
			if m == method {
				out = append(out, c)
				break
			}
		}
	}
	return out
}

func (a *App) Quote(ctx context.Context, code string, method string, items []CartItem) (Quote, error) {
	batch, err := a.Q.GetBatchByCode(ctx, code)
	if err != nil {
		return Quote{}, err
	}
	lines, sub, err := a.priceLines(ctx, batch.ID, items)
	if err != nil {
		return Quote{}, err
	}
	charges, err := a.Q.ListActiveCharges(ctx, batch.BrandID)
	if err != nil {
		return Quote{}, err
	}
	applied := applicableCharges(charges, method)
	var extra int64
	for _, c := range applied {
		extra += c.AmountRp
	}
	return Quote{Subtotal: sub, Charges: applied, Extra: extra, Base: sub + extra, Lines: lines}, nil
}

func (a *App) priceLines(ctx context.Context, batchID uuid.UUID, items []CartItem) ([]CartItem, int64, error) {
	if len(items) == 0 {
		return nil, 0, E(400, "empty_cart", "Keranjang masih kosong.")
	}
	merged := map[uuid.UUID]int32{}
	var order []uuid.UUID
	for _, it := range items {
		if it.Qty <= 0 {
			return nil, 0, E(400, "bad_qty", "Jumlah pesanan belum pas.")
		}
		if _, ok := merged[it.BatchItemID]; !ok {
			order = append(order, it.BatchItemID)
		}
		merged[it.BatchItemID] += it.Qty
	}
	rows, err := a.Q.ListBatchItems(ctx, batchID)
	if err != nil {
		return nil, 0, err
	}
	byID := map[uuid.UUID]db.BatchItem{}
	for _, r := range rows {
		byID[r.ID] = r
	}
	var out []CartItem
	var sub int64
	for _, id := range order {
		row, ok := byID[id]
		if !ok {
			return nil, 0, E(400, "bad_item", "Ada item yang bukan dari PO ini.")
		}
		qty := merged[id]
		out = append(out, CartItem{BatchItemID: id, Qty: qty})
		sub += row.Price * int64(qty)
	}
	return out, sub, nil
}

func (a *App) PlaceOrder(ctx context.Context, code string, in PlaceOrder) (db.Order, error) {
	if in.IdempotencyKey != "" {
		key := in.IdempotencyKey
		if existing, err := a.Q.GetOrderByIdempotency(ctx, &key); err == nil {
			return existing, nil
		}
	}
	wa, err := text.WhatsApp(in.WhatsApp)
	if err != nil || strings.TrimSpace(in.Name) == "" {
		return db.Order{}, E(400, "bad_customer", "Isi nama dan nomor WhatsApp yang aktif ya.")
	}
	if in.Method != "pickup" && in.Method != "delivery" {
		return db.Order{}, E(400, "bad_method", "Pilih ambil sendiri atau diantar.")
	}
	if in.Method == "delivery" && strings.TrimSpace(in.Address) == "" {
		return db.Order{}, E(400, "need_address", "Isi alamat antar dulu.")
	}
	batch, err := a.Q.GetBatchByCode(ctx, code)
	if errors.Is(err, pgx.ErrNoRows) {
		return db.Order{}, E(404, "not_found", "PO tidak ketemu.")
	}
	if err != nil {
		return db.Order{}, err
	}
	settings, err := a.LoadSettings(ctx)
	if err != nil {
		return db.Order{}, err
	}
	var created db.Order
	err = a.txConn(ctx, func(q *db.Queries, tx pgx.Tx) error {
		b, err := q.GetBatchForUpdate(ctx, batch.ID)
		if err != nil {
			return err
		}
		if b.Status != "open" {
			return E(409, "batch_closed", "PO ini sudah ditutup.")
		}
		if in.Method == "pickup" && !b.AllowPickup {
			return E(400, "bad_method", "PO ini tidak bisa diambil sendiri.")
		}
		if in.Method == "delivery" && !b.AllowDelivery {
			return E(400, "bad_method", "PO ini tidak diantar.")
		}
		store, err := q.GetStore(ctx, b.StoreID)
		if err != nil {
			return err
		}
		if !store.IsOpen && !store.AcceptPreordersWhenClosed {
			return E(409, "store_closed", "Toko sedang tutup dan belum terima pre-order.")
		}
		lines, sub, err := priceLocked(ctx, q, b.ID, in.Items)
		if err != nil {
			return err
		}
		if b.QuotaTotal != nil {
			var sold int32
			rows, err := q.ListBatchItems(ctx, b.ID)
			if err != nil {
				return err
			}
			for _, r := range rows {
				sold += r.SoldCount
			}
			var add int32
			for _, l := range lines {
				add += l.Qty
			}
			if sold+add > *b.QuotaTotal {
				return E(409, "quota", "Kuota PO ini tinggal sedikit. Kurangi jumlahnya ya.")
			}
		}
		charges, err := q.ListActiveCharges(ctx, b.BrandID)
		if err != nil {
			return err
		}
		applied := applicableCharges(charges, in.Method)
		var extra int64
		for _, c := range applied {
			extra += c.AmountRp
		}
		base := sub + extra
		banks, err := q.ListBanks(ctx, b.BrandID)
		if err != nil {
			return err
		}
		bank, err := defaultBank(banks)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", lockKey(bank.ID)); err != nil {
			return err
		}
		reserved, err := q.ListReservedTotals(ctx, bank.ID)
		if err != nil {
			return err
		}
		taken := map[int64]struct{}{}
		for _, t := range reserved {
			taken[t] = struct{}{}
		}
		cust, err := q.UpsertCustomer(ctx, db.UpsertCustomerParams{BrandID: b.BrandID, Name: strings.TrimSpace(in.Name), WaNumberE164: wa})
		if err != nil {
			return err
		}
		var key *string
		if in.IdempotencyKey != "" {
			key = &in.IdempotencyKey
		}
		var absolute *time.Time
		if b.PaymentDeadlineAt.Valid {
			t := b.PaymentDeadlineAt.Time
			absolute = &t
		}
		exp := paymentExpiry(time.Now(), b.ClosesAt, b.PaymentDeadlineMins, absolute, settings.PaymentDeadlineHours)
		slots := ParseSlots(b.PickupSlots)
		var slotStart, slotEnd *time.Time
		if len(slots) > 0 {
			idx := in.SlotIndex
			if idx < 0 || idx >= len(slots) {
				return E(400, "bad_slot", "Pilih slot ambil yang tersedia.")
			}
			slotStart = &slots[idx].Start
			slotEnd = &slots[idx].End
		}
		var order db.Order
		for try := 0; try < 5; try++ {
			suffix, total, err := uniqueamount.Pick(base, taken)
			if err != nil {
				return E(409, "no_suffix", "Kode unik lagi penuh. Minta penjual menambah rekening.")
			}
			if _, err := tx.Exec(ctx, "SAVEPOINT ins_order"); err != nil {
				return err
			}
			codeStr, err := q.NextOrderCode(ctx)
			if err != nil {
				return err
			}
			token, err := publicToken()
			if err != nil {
				return err
			}
			order, err = q.InsertOrder(ctx, db.InsertOrderParams{
				BatchID: b.ID, BrandID: b.BrandID, StoreID: b.StoreID, CustomerID: cust.ID,
				Code: codeStr, PublicToken: token, IdempotencyKey: key,
				Subtotal: sub, ExtraChargesTotal: extra, BaseTotal: base,
				UniqueSuffix: suffix, TotalDue: total, BankAccountID: bank.ID,
				FulfilmentMethod: in.Method, Address: strings.TrimSpace(in.Address), Note: strings.TrimSpace(in.Note),
				ExpiresAt: pgtPtr(exp), PickupSlotStart: pgtPtr(slotStart), PickupSlotEnd: pgtPtr(slotEnd),
			})
			if err == nil {
				if _, err := tx.Exec(ctx, "RELEASE SAVEPOINT ins_order"); err != nil {
					return err
				}
				break
			}
			if _, rbErr := tx.Exec(ctx, "ROLLBACK TO SAVEPOINT ins_order"); rbErr != nil {
				return rbErr
			}
			if isUnique(err, "ux_orders_active_amount") {
				taken[total] = struct{}{}
				order = db.Order{}
				continue
			}
			if isUnique(err, "orders_idempotency_key_key") {
				return errIdempotent
			}
			return err
		}
		if order.ID == uuid.Nil {
			return E(409, "no_suffix", "Kode unik lagi penuh. Coba sekali lagi ya.")
		}
		for i, l := range lines {
			row, err := q.GetBatchItemForUpdate(ctx, l.BatchItemID)
			if err != nil {
				return err
			}
			if _, err := q.AddSoldCount(ctx, db.AddSoldCountParams{ID: row.ID, SoldCount: l.Qty}); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return E(409, "quota", row.ProductName+" habis.")
				}
				return err
			}
			if _, err := q.InsertOrderItem(ctx, db.InsertOrderItemParams{
				OrderID: order.ID, BatchItemID: row.ID, ProductName: row.ProductName, VariantName: row.VariantName,
				UnitPrice: row.Price, Qty: l.Qty,
			}); err != nil {
				return err
			}
			_ = i
		}
		for i, c := range applied {
			if _, err := q.InsertOrderCharge(ctx, db.InsertOrderChargeParams{
				OrderID: order.ID, ExtraChargeID: pgu(c.ID), Name: c.Name, AmountRp: c.AmountRp, ComputedRp: c.AmountRp, SortOrder: int32(i),
			}); err != nil {
				return err
			}
		}
		if err := maybeCloseBatch(ctx, q, b.ID); err != nil {
			return err
		}
		created = order
		return nil
	})
	if errors.Is(err, errIdempotent) && in.IdempotencyKey != "" {
		key := in.IdempotencyKey
		return a.Q.GetOrderByIdempotency(ctx, &key)
	}
	if err != nil {
		return db.Order{}, err
	}
	a.notifyBrandOwner(ctx, created.BrandID, "order_created", "Pesanan baru",
		"Ping! Ada pesanan baru dari "+in.Name+".",
		"/app/pesanan/"+created.ID.String(), "order_created:"+created.ID.String(), true)
	return created, nil
}

var errIdempotent = errors.New("idempotent replay")

func priceLocked(ctx context.Context, q *db.Queries, batchID uuid.UUID, items []CartItem) ([]CartItem, int64, error) {
	rows, err := q.ListBatchItemsForUpdate(ctx, batchID)
	if err != nil {
		return nil, 0, err
	}
	byID := map[uuid.UUID]db.BatchItem{}
	for _, r := range rows {
		byID[r.ID] = r
	}
	merged := map[uuid.UUID]int32{}
	var orderIDs []uuid.UUID
	for _, it := range items {
		if it.Qty <= 0 {
			return nil, 0, E(400, "bad_qty", "Jumlah pesanan belum pas.")
		}
		if _, ok := merged[it.BatchItemID]; !ok {
			orderIDs = append(orderIDs, it.BatchItemID)
		}
		merged[it.BatchItemID] += it.Qty
	}
	if len(orderIDs) == 0 {
		return nil, 0, E(400, "empty_cart", "Keranjang masih kosong.")
	}
	var out []CartItem
	var sub int64
	for _, id := range orderIDs {
		row, ok := byID[id]
		if !ok {
			return nil, 0, E(400, "bad_item", "Ada item yang bukan dari PO ini.")
		}
		qty := merged[id]
		if row.Quota != nil && row.SoldCount+qty > *row.Quota {
			return nil, 0, E(409, "quota", row.ProductName+" sisa "+fmt.Sprint(*row.Quota-row.SoldCount)+".")
		}
		out = append(out, CartItem{BatchItemID: id, Qty: qty})
		sub += row.Price * int64(qty)
	}
	return out, sub, nil
}

func defaultBank(banks []db.BankAccount) (db.BankAccount, error) {
	for _, b := range banks {
		if b.IsDefault && b.Active {
			return b, nil
		}
	}
	for _, b := range banks {
		if b.Active {
			return b, nil
		}
	}
	return db.BankAccount{}, E(400, "no_bank", "Penjual belum memasang rekening.")
}

func paymentExpiry(now, closes time.Time, mins *int32, absolute *time.Time, defaultHours int) *time.Time {
	if mins != nil && *mins == 0 && absolute == nil {
		return nil
	}
	if absolute != nil && absolute.After(now) {
		t := absolute.UTC()
		return &t
	}
	var deadline time.Time
	if mins != nil && *mins > 0 {
		deadline = now.Add(time.Duration(*mins) * time.Minute)
	} else {
		h := defaultHours
		if h <= 0 {
			h = 24
		}
		deadline = now.Add(time.Duration(h) * time.Hour)
	}
	cutoff := closes.Add(2 * time.Hour)
	if deadline.After(cutoff) {
		deadline = cutoff
	}
	min := now.Add(15 * time.Minute)
	if deadline.Before(min) {
		deadline = min
	}
	return &deadline
}

func maybeCloseBatch(ctx context.Context, q *db.Queries, batchID uuid.UUID) error {
	b, err := q.GetBatch(ctx, batchID)
	if err != nil {
		return err
	}
	items, err := q.ListBatchItems(ctx, batchID)
	if err != nil {
		return err
	}
	var sold int32
	allLimited := true
	allGone := true
	for _, it := range items {
		sold += it.SoldCount
		if it.Quota == nil {
			allLimited = false
			allGone = false
			continue
		}
		if it.SoldCount < *it.Quota {
			allGone = false
		}
	}
	closeIt := false
	if b.QuotaTotal != nil && sold >= *b.QuotaTotal {
		closeIt = true
	}
	if allLimited && allGone && len(items) > 0 {
		closeIt = true
	}
	if closeIt && b.Status == "open" {
		_, err = q.SetBatchStatus(ctx, db.SetBatchStatusParams{ID: batchID, Status: "closed"})
	}
	return err
}

func (a *App) LoadOrder(ctx context.Context, id uuid.UUID) (OrderDetail, error) {
	o, err := a.Q.GetOrder(ctx, id)
	if err != nil {
		return OrderDetail{}, err
	}
	return a.hydrate(ctx, o)
}

func (a *App) LoadOrderByToken(ctx context.Context, token string) (OrderDetail, error) {
	o, err := a.Q.GetOrderByToken(ctx, token)
	if errors.Is(err, pgx.ErrNoRows) {
		return OrderDetail{}, E(404, "not_found", "Pesanan tidak ketemu.")
	}
	if err != nil {
		return OrderDetail{}, err
	}
	return a.hydrate(ctx, o)
}

func (a *App) hydrate(ctx context.Context, o db.Order) (OrderDetail, error) {
	items, err := a.Q.ListOrderItems(ctx, o.ID)
	if err != nil {
		return OrderDetail{}, err
	}
	charges, err := a.Q.ListOrderCharges(ctx, o.ID)
	if err != nil {
		return OrderDetail{}, err
	}
	cust, err := a.Q.GetCustomer(ctx, o.CustomerID)
	if err != nil {
		return OrderDetail{}, err
	}
	bank, err := a.Q.GetBank(ctx, o.BankAccountID)
	if err != nil {
		return OrderDetail{}, err
	}
	proofs, err := a.Q.ListProofsByOrder(ctx, pgu(o.ID))
	if err != nil {
		return OrderDetail{}, err
	}
	batch, err := a.Q.GetBatch(ctx, o.BatchID)
	if err != nil {
		return OrderDetail{}, err
	}
	store, err := a.Q.GetStore(ctx, o.StoreID)
	if err != nil {
		return OrderDetail{}, err
	}
	brand, err := a.Q.GetBrandByID(ctx, o.BrandID)
	if err != nil {
		return OrderDetail{}, err
	}
	return OrderDetail{Order: o, Items: items, Charges: charges, Customer: cust, Bank: bank, Proofs: proofs, Batch: batch, Store: store, Brand: brand}, nil
}

func (a *App) SaveProof(ctx context.Context, token string, r io.Reader, mime string, size int) error {
	if size <= 0 || size > 2_500_000 {
		return E(400, "bad_file", "Ukuran bukti maksimal 2,5 MB.")
	}
	switch mime {
	case "image/jpeg", "image/png", "image/webp":
	default:
		return E(400, "bad_file", "Bukti harus JPG, PNG, atau WebP.")
	}
	var orderID uuid.UUID
	var brandID uuid.UUID
	var code string
	var name string
	err := a.tx(ctx, func(q *db.Queries) error {
		o, err := q.GetOrderByTokenForUpdate(ctx, token)
		if errors.Is(err, pgx.ErrNoRows) {
			return E(404, "not_found", "Pesanan tidak ketemu.")
		}
		if err != nil {
			return err
		}
		if !orderflow.CanUploadProof(o.PaymentStatus) {
			return E(409, "bad_state", "Pesanan ini sudah tidak menerima bukti.")
		}
		ext := "jpg"
		if mime == "image/png" {
			ext = "png"
		} else if mime == "image/webp" {
			ext = "webp"
		}
		key := "proofs/" + uuid.NewString() + "." + ext
		if err := a.Blob.Put(ctx, key, r, mime); err != nil {
			return err
		}
		if _, err := q.InsertProof(ctx, db.InsertProofParams{
			OrderID: pgu(o.ID), ObjectKey: key, Mime: mime, SizeBytes: int32(size),
		}); err != nil {
			return err
		}
		if _, err := q.MarkOrderProof(ctx, o.ID); err != nil {
			return err
		}
		orderID, brandID, code = o.ID, o.BrandID, o.Code
		cust, err := q.GetCustomer(ctx, o.CustomerID)
		if err == nil {
			name = cust.Name
		}
		return nil
	})
	if err != nil {
		return err
	}
	a.notifyBrandOwner(ctx, brandID, "proof_uploaded", "Bukti transfer",
		"Ping! Bukti transfer masuk, cek yuk. "+name+" · "+code+".",
		"/app/pesanan/"+orderID.String(), "proof:"+orderID.String()+":"+uuid.NewString(), true)
	return nil
}

func (a *App) MarkPaid(ctx context.Context, actor, orderID uuid.UUID) (db.Order, error) {
	var out db.Order
	var brandID uuid.UUID
	err := a.tx(ctx, func(q *db.Queries) error {
		o, err := q.GetOrderForUpdate(ctx, orderID)
		if err != nil {
			return err
		}
		brand, acc, err := a.ownedInTx(ctx, q, actor, o.BrandID)
		if err != nil {
			return err
		}
		if !orderflow.CanPay(o.PaymentStatus) {
			return E(409, "bad_state", "Pesanan ini tidak bisa ditandai lunas.")
		}
		settings, err := a.LoadSettings(ctx)
		if err != nil {
			return err
		}
		w, err := q.GetWalletForAccountUpdate(ctx, acc.ID)
		if err != nil {
			return err
		}
		now := time.Now()
		if _, _, err := applyLedger(ctx, q, w.ID, ledgerIn{
			Type: "order_debit", Amount: -settings.OrderFee, Key: "order_debit:" + o.ID.String(),
			OrderID: &o.ID, BrandID: &brand.ID, Actor: &actor, Reason: "Pesanan " + o.Code,
		}); err != nil {
			return err
		}
		out, err = q.SetOrderPayment(ctx, db.SetOrderPaymentParams{
			ID: o.ID, PaymentStatus: "paid", PaidAt: pgt(now), ChargedAt: pgt(now),
		})
		if err != nil {
			return err
		}
		if err := q.AddCustomerSpend(ctx, db.AddCustomerSpendParams{ID: o.CustomerID, TotalSpent: o.TotalDue}); err != nil {
			return err
		}
		_ = q.SetProofReview(ctx, db.SetProofReviewParams{OrderID: pgu(o.ID), ReviewStatus: "accepted", ReviewedBy: pgu(actor)})
		a.audit(ctx, q, &acc.ID, &actor, "order.paid", "order", o.ID.String(), map[string]string{"status": o.PaymentStatus}, map[string]any{"status": "paid", "fee": settings.OrderFee})
		brandID = brand.ID
		return nil
	})
	_ = brandID
	return out, err
}

// UndoPaid puts a paid order back to proof_submitted without refunding koin.
// Marking it paid again does not debit a second time.
func (a *App) UndoPaid(ctx context.Context, actor, orderID uuid.UUID) (db.Order, error) {
	var out db.Order
	err := a.tx(ctx, func(q *db.Queries) error {
		o, err := q.GetOrderForUpdate(ctx, orderID)
		if err != nil {
			return err
		}
		if _, _, err := a.ownedInTx(ctx, q, actor, o.BrandID); err != nil {
			return err
		}
		if o.PaymentStatus != "paid" {
			return E(409, "bad_state", "Cuma pesanan lunas yang bisa dikembalikan ke antrean cek.")
		}
		out, err = q.SetOrderPayment(ctx, db.SetOrderPaymentParams{
			ID: o.ID, PaymentStatus: "proof_submitted", PaidAt: pgtype.Timestamptz{}, ChargedAt: o.ChargedAt,
		})
		return err
	})
	return out, err
}

func (a *App) RejectProof(ctx context.Context, actor, orderID uuid.UUID, reason string) (db.Order, error) {
	if strings.TrimSpace(reason) == "" {
		return db.Order{}, E(400, "need_reason", "Isi alasan penolakan.")
	}
	var out db.Order
	err := a.tx(ctx, func(q *db.Queries) error {
		o, err := q.GetOrderForUpdate(ctx, orderID)
		if err != nil {
			return err
		}
		if _, _, err := a.ownedInTx(ctx, q, actor, o.BrandID); err != nil {
			return err
		}
		if !orderflow.CanRejectProof(o.PaymentStatus) {
			return E(409, "bad_state", "Tidak ada bukti yang bisa ditolak.")
		}
		if err := q.SetProofReview(ctx, db.SetProofReviewParams{
			OrderID: pgu(o.ID), ReviewStatus: "rejected", RejectReason: &reason, ReviewedBy: pgu(actor),
		}); err != nil {
			return err
		}
		out, err = q.SetOrderPayment(ctx, db.SetOrderPaymentParams{ID: o.ID, PaymentStatus: "pending"})
		return err
	})
	return out, err
}

func (a *App) CancelOrder(ctx context.Context, actor, orderID uuid.UUID) (db.Order, error) {
	var out db.Order
	err := a.tx(ctx, func(q *db.Queries) error {
		o, err := q.GetOrderForUpdate(ctx, orderID)
		if err != nil {
			return err
		}
		_, acc, err := a.ownedInTx(ctx, q, actor, o.BrandID)
		if err != nil {
			return err
		}
		if !orderflow.CanCancel(o.PaymentStatus) {
			return E(409, "bad_state", "Pesanan ini tidak bisa dibatalkan.")
		}
		now := time.Now()
		if o.PaymentStatus == "paid" {
			if !o.PaidAt.Valid || !refundOK(o.PaidAt.Time, now) {
				return E(409, "refund_window", "Pengembalian koin cuma berlaku 24 jam setelah lunas.")
			}
			settings, err := a.LoadSettings(ctx)
			if err != nil {
				return err
			}
			w, err := q.GetWalletForAccountUpdate(ctx, acc.ID)
			if err != nil {
				return err
			}
			if _, _, err := applyLedger(ctx, q, w.ID, ledgerIn{
				Type: "refund_credit", Amount: settings.OrderFee, Key: "refund_credit:" + o.ID.String(),
				OrderID: &o.ID, BrandID: &o.BrandID, Actor: &actor, Reason: "Batal " + o.Code,
			}); err != nil {
				return err
			}
			if err := q.AddCustomerSpend(ctx, db.AddCustomerSpendParams{ID: o.CustomerID, TotalSpent: -o.TotalDue}); err != nil {
				return err
			}
		}
		if orderflow.ReleasesQuota(o.PaymentStatus) {
			if err := releaseItems(ctx, q, o.ID); err != nil {
				return err
			}
		}
		out, err = q.SetOrderPayment(ctx, db.SetOrderPaymentParams{
			ID: o.ID, PaymentStatus: "cancelled", PaidAt: o.PaidAt, ChargedAt: o.ChargedAt, CancelledAt: pgt(now),
		})
		if err != nil {
			return err
		}
		a.audit(ctx, q, &acc.ID, &actor, "order.cancel", "order", o.ID.String(), map[string]string{"status": o.PaymentStatus}, map[string]string{"status": "cancelled"})
		return nil
	})
	return out, err
}

func refundOK(paidAt, now time.Time) bool {
	return !now.After(paidAt.Add(24 * time.Hour))
}

func releaseItems(ctx context.Context, q *db.Queries, orderID uuid.UUID) error {
	items, err := q.ListOrderItems(ctx, orderID)
	if err != nil {
		return err
	}
	for _, it := range items {
		if err := q.ReleaseSoldCount(ctx, db.ReleaseSoldCountParams{ID: it.BatchItemID, SoldCount: it.Qty}); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) SetFulfilment(ctx context.Context, actor, orderID uuid.UUID, to string) (db.Order, error) {
	var out db.Order
	err := a.tx(ctx, func(q *db.Queries) error {
		o, err := q.GetOrderForUpdate(ctx, orderID)
		if err != nil {
			return err
		}
		if _, _, err := a.ownedInTx(ctx, q, actor, o.BrandID); err != nil {
			return err
		}
		if !orderflow.CanFulfil(o.PaymentStatus, o.FulfilmentStatus, to) {
			return E(409, "bad_state", "Status ambil belum bisa diubah ke situ.")
		}
		out, err = q.SetOrderFulfilment(ctx, db.SetOrderFulfilmentParams{ID: o.ID, FulfilmentStatus: to})
		return err
	})
	return out, err
}

func (a *App) ownedInTx(ctx context.Context, q *db.Queries, userID, brandID uuid.UUID) (db.Brand, db.Account, error) {
	acc, err := q.GetAccountByOwner(ctx, userID)
	if err != nil {
		return db.Brand{}, db.Account{}, E(403, "forbidden", "Bukan pesanan kamu.")
	}
	brand, err := q.GetBrandByID(ctx, brandID)
	if err != nil {
		return db.Brand{}, acc, err
	}
	if brand.AccountID != acc.ID {
		return db.Brand{}, acc, E(403, "forbidden", "Bukan pesanan kamu.")
	}
	return brand, acc, nil
}

func (a *App) WALink(ctx context.Context, actor, orderID uuid.UUID, kind string) (string, error) {
	d, err := a.LoadOrder(ctx, orderID)
	if err != nil {
		return "", err
	}
	if _, _, err := a.OwnedBrand(ctx, actor, d.Order.BrandID); err != nil {
		return "", err
	}
	if kind == "" {
		kind = "confirmation"
	}
	body := defaultConfirm
	switch kind {
	case "reminder":
		body = defaultRemind
	case "ready":
		body = defaultReady
	}
	if tpl, err := a.Q.GetTemplate(ctx, db.GetTemplateParams{BrandID: d.Brand.ID, Kind: kind}); err == nil {
		body = tpl.Body
	}
	batas := "tanpa batas"
	if d.Order.ExpiresAt.Valid {
		batas = jakarta(d.Order.ExpiresAt.Time)
	}
	filled := text.Fill(body, map[string]string{
		"nama":        d.Customer.Name,
		"kode":        d.Order.Code,
		"brand":       d.Brand.Name,
		"rincian":     rincian(d.Items, d.Charges, d.Order.UniqueSuffix),
		"total":       text.Rupiah(d.Order.TotalDue),
		"bank":        d.Bank.BankCode,
		"norek":       d.Bank.AccountNumber,
		"pemilik":     d.Bank.HolderName,
		"link_status": a.Cfg.PublicOrigin + "/o/" + d.Order.PublicToken,
		"batas_bayar": batas,
		"store":       d.Store.Name,
		"alamat":      d.Store.Address,
	})
	return "https://wa.me/" + text.WAForLink(d.Customer.WaNumberE164) + "?text=" + url.QueryEscape(filled), nil
}

func rincian(items []db.OrderItem, charges []db.OrderCharge, suffix int32) string {
	var b strings.Builder
	for _, it := range items {
		name := it.ProductName
		if it.VariantName != nil && *it.VariantName != "" {
			name += " " + *it.VariantName
		}
		fmt.Fprintf(&b, "%dx %s  Rp %s\n", it.Qty, name, text.Rupiah(it.UnitPrice*int64(it.Qty)))
	}
	for _, c := range charges {
		fmt.Fprintf(&b, "%s  Rp %s\n", c.Name, text.Rupiah(c.ComputedRp))
	}
	fmt.Fprintf(&b, "Kode unik  Rp %s", text.Rupiah(int64(suffix)))
	return b.String()
}

type Recap struct {
	Batch         db.PoBatch
	Products      []RecapProduct
	Charges       []RecapCharge
	Checklist     []CheckRow
	PaidOrders    int
	IncludeUnpaid bool
	Locked        bool
}

type RecapProduct struct {
	Name    string
	Variant string
	Qty     int32
	Photo   string
}

type RecapCharge struct {
	Name  string
	Count int
	Total int64
}

type CheckRow struct {
	OrderID uuid.UUID
	Code    string
	Name    string
	Summary string
	Method  string
	Address string
	Status  string
	Paid    bool
	Orders  int32
}

func (a *App) Recap(ctx context.Context, actor, batchID uuid.UUID, includeUnpaid bool) (Recap, error) {
	batch, err := a.Q.GetBatch(ctx, batchID)
	if err != nil {
		return Recap{}, err
	}
	_, acc, err := a.OwnedBrand(ctx, actor, batch.BrandID)
	if err != nil {
		return Recap{}, err
	}
	locked, _, _, err := a.softLocked(ctx, acc.ID)
	if err != nil {
		return Recap{}, err
	}
	if locked {
		return Recap{Batch: batch, Locked: true}, E(402, "soft_lock", "Isi saldo untuk membuka")
	}
	orders, err := a.Q.ListOrdersByBatch(ctx, batch.ID)
	if err != nil {
		return Recap{}, err
	}
	items, err := a.Q.ListBatchItems(ctx, batch.ID)
	if err != nil {
		return Recap{}, err
	}
	photos := map[uuid.UUID]string{}
	for _, it := range items {
		p, err := a.Q.GetProduct(ctx, it.ProductID)
		if err == nil && p.PhotoUrl != nil {
			photos[it.ID] = *p.PhotoUrl
		}
	}
	prod := map[string]*RecapProduct{}
	var prodOrder []string
	charge := map[string]*RecapCharge{}
	var chOrder []string
	var checks []CheckRow
	paidN := 0
	for _, o := range orders {
		if o.PaymentStatus == "cancelled" || o.PaymentStatus == "expired" {
			continue
		}
		paid := o.PaymentStatus == "paid"
		if paid {
			paidN++
		}
		if !includeUnpaid && !paid {
			continue
		}
		lines, err := a.Q.ListOrderItems(ctx, o.ID)
		if err != nil {
			return Recap{}, err
		}
		var summary []string
		for _, ln := range lines {
			key := ln.ProductName + "|" + strPtr(ln.VariantName)
			if _, ok := prod[key]; !ok {
				prod[key] = &RecapProduct{Name: ln.ProductName, Variant: strPtr(ln.VariantName), Photo: photos[ln.BatchItemID]}
				prodOrder = append(prodOrder, key)
			}
			prod[key].Qty += ln.Qty
			label := ln.ProductName
			if ln.VariantName != nil && *ln.VariantName != "" {
				label += " " + *ln.VariantName
			}
			summary = append(summary, fmt.Sprintf("%d %s", ln.Qty, label))
		}
		chs, err := a.Q.ListOrderCharges(ctx, o.ID)
		if err != nil {
			return Recap{}, err
		}
		for _, c := range chs {
			if _, ok := charge[c.Name]; !ok {
				charge[c.Name] = &RecapCharge{Name: c.Name}
				chOrder = append(chOrder, c.Name)
			}
			charge[c.Name].Count++
			charge[c.Name].Total += c.ComputedRp
		}
		cust, err := a.Q.GetCustomer(ctx, o.CustomerID)
		if err != nil {
			return Recap{}, err
		}
		checks = append(checks, CheckRow{
			OrderID: o.ID, Code: o.Code, Name: cust.Name, Summary: strings.Join(summary, " · "),
			Method: o.FulfilmentMethod, Address: o.Address, Status: o.FulfilmentStatus, Paid: paid, Orders: cust.OrdersCount,
		})
	}
	out := Recap{Batch: batch, Checklist: checks, PaidOrders: paidN, IncludeUnpaid: includeUnpaid}
	for _, k := range prodOrder {
		out.Products = append(out.Products, *prod[k])
	}
	for _, k := range chOrder {
		out.Charges = append(out.Charges, *charge[k])
	}
	return out, nil
}

func (a *App) RecapCSV(ctx context.Context, actor, batchID uuid.UUID, includeUnpaid bool) (string, error) {
	rec, err := a.Recap(ctx, actor, batchID, includeUnpaid)
	if err != nil {
		return "", err
	}
	var b bytes.Buffer
	b.WriteString("kode,nama,item,metode,alamat,status_bayar,status_ambil\n")
	for _, row := range rec.Checklist {
		pay := "belum"
		if row.Paid {
			pay = "lunas"
		}
		fmt.Fprintf(&b, "%s,%s,%s,%s,%s,%s,%s\n", csv(row.Code), csv(row.Name), csv(row.Summary), row.Method, csv(row.Address), pay, row.Status)
	}
	return b.String(), nil
}

func csv(s string) string {
	if strings.ContainsAny(s, ",\"\n") {
		return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
	}
	return s
}

func strPtr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func (a *App) ListOrders(ctx context.Context, actor, brandID uuid.UUID, status string, amount *int64, batchID *uuid.UUID) ([]db.Order, error) {
	if _, _, err := a.OwnedBrand(ctx, actor, brandID); err != nil {
		return nil, err
	}
	var st *string
	if status != "" {
		st = &status
	}
	bid := pgtype.UUID{}
	if batchID != nil {
		bid = pgu(*batchID)
	}
	return a.Q.ListBrandOrders(ctx, db.ListBrandOrdersParams{
		BrandID: brandID, PaymentStatus: st, TotalDue: amount, BatchID: bid,
	})
}
