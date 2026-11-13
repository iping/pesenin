package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"pesenin.product.biz.id/api/internal/db"
	"pesenin.product.biz.id/api/internal/text"
)

// lockCapacityRows locks the batch counters before item rows.
// Checkout, expiry, and seller edits all use this order: batch, capacity, items.
func lockCapacityRows(ctx context.Context, tx pgx.Tx, batchID uuid.UUID) error {
	rows, err := tx.Query(ctx, `SELECT batch_id FROM batch_capacities WHERE batch_id = $1 FOR UPDATE`, batchID)
	if err != nil {
		return err
	}
	rows.Close()
	rows, err = tx.Query(ctx, `SELECT slot_start FROM pickup_capacities WHERE batch_id = $1 ORDER BY slot_start FOR UPDATE`, batchID)
	if err != nil {
		return err
	}
	rows.Close()
	return nil
}

func lineQty(lines []CartItem) int32 {
	var n int32
	for _, l := range lines {
		n += l.Qty
	}
	return n
}

func customerQty(ctx context.Context, tx pgx.Tx, batchID, customerID uuid.UUID, item *uuid.UUID) (int32, error) {
	var n int32
	err := tx.QueryRow(ctx, `
		SELECT COALESCE(SUM(oi.qty), 0)::int
		FROM order_items oi
		JOIN orders o ON o.id = oi.order_id
		WHERE o.batch_id = $1
		  AND o.customer_id = $2
		  AND o.payment_status NOT IN ('cancelled', 'expired')
		  AND ($3::uuid IS NULL OR oi.batch_item_id = $3)
	`, batchID, customerID, item).Scan(&n)
	return n, err
}

// applyCapacity checks mins, maxes, cutoff, and reserves order-count and pickup-slot capacity.
// Item qty is reserved by AddSoldCount. The batch row is already locked by the caller.
func applyCapacity(ctx context.Context, tx pgx.Tx, b db.PoBatch, customer uuid.UUID, lines []CartItem, sub int64, slot *time.Time) error {
	if !b.ClosesAt.After(time.Now()) {
		return E(409, "batch_closed", "PO ini sudah ditutup.")
	}
	row, err := loadBatchCap(ctx, tx, b.ID, false)
	if err != nil {
		return err
	}
	add := lineQty(lines)
	if row.found {
		if err := rejectCutoff(row.capRule, b, slot); err != nil {
			return err
		}
		if row.MinQty != nil && add < *row.MinQty {
			return E(400, "below_min_qty", msgMinQty(*row.MinQty))
		}
		if row.MinAmountRp != nil && sub < *row.MinAmountRp {
			return E(400, "below_min_amount", msgMinRp(*row.MinAmountRp))
		}
		if row.MaxQtyPerCustomer != nil {
			have, err := customerQty(ctx, tx, b.ID, customer, nil)
			if err != nil {
				return err
			}
			if have+add > *row.MaxQtyPerCustomer {
				return E(409, "above_max", msgMax(*row.MaxQtyPerCustomer))
			}
		}
	}
	limits, err := loadItemLimits(ctx, tx, b.ID)
	if err != nil {
		return err
	}
	names := map[uuid.UUID]string{}
	itemRows, err := tx.Query(ctx, `SELECT id, product_name FROM batch_items WHERE batch_id = $1`, b.ID)
	if err != nil {
		return err
	}
	for itemRows.Next() {
		var id uuid.UUID
		var name string
		if err := itemRows.Scan(&id, &name); err != nil {
			itemRows.Close()
			return err
		}
		names[id] = name
	}
	itemRows.Close()
	if err := itemRows.Err(); err != nil {
		return err
	}
	for _, l := range lines {
		lim := limits[l.BatchItemID]
		if lim.MinQty != nil && l.Qty < *lim.MinQty {
			return E(400, "below_min_qty", names[l.BatchItemID]+": "+msgMinQty(*lim.MinQty))
		}
		if lim.MaxPerCustomer == nil {
			continue
		}
		have, err := customerQty(ctx, tx, b.ID, customer, &l.BatchItemID)
		if err != nil {
			return err
		}
		if have+l.Qty > *lim.MaxPerCustomer {
			return E(409, "above_max", msgMaxItem(names[l.BatchItemID], *lim.MaxPerCustomer))
		}
	}
	if b.QuotaTotal != nil {
		held, err := heldQty(ctx, tx, b.ID)
		if err != nil {
			return err
		}
		if held+add > *b.QuotaTotal {
			left := *b.QuotaTotal - held
			if left < 0 {
				left = 0
			}
			return E(409, "quota", msgBatchLeft(left))
		}
	}
	if row.found && row.CapacityOrders != nil {
		var reserved int32
		err := tx.QueryRow(ctx, `
			UPDATE batch_capacities
			SET reserved_orders = reserved_orders + 1, updated_at = now()
			WHERE batch_id = $1
			  AND (capacity_orders IS NULL OR reserved_orders + 1 <= capacity_orders)
			RETURNING reserved_orders
		`, b.ID).Scan(&reserved)
		if errors.Is(err, pgx.ErrNoRows) {
			return E(409, "capacity", msgOrdersFull())
		}
		if err != nil {
			return err
		}
	} else if row.found {
		if _, err := tx.Exec(ctx, `UPDATE batch_capacities SET reserved_orders = reserved_orders + 1, updated_at = now() WHERE batch_id = $1`, b.ID); err != nil {
			return err
		}
	}
	if slot != nil {
		if err := reserveSlot(ctx, tx, b.ID, *slot, add); err != nil {
			return err
		}
	}
	return nil
}

func rejectCutoff(rule capRule, b db.PoBatch, slot *time.Time) error {
	if rule.CutoffLeadMinutes == nil || *rule.CutoffLeadMinutes <= 0 {
		return nil
	}
	anchor := b.ReadyAt
	if slot != nil {
		anchor = *slot
	}
	deadline := anchor.Add(-time.Duration(*rule.CutoffLeadMinutes) * time.Minute)
	if !time.Now().Before(deadline) {
		return E(409, "cutoff", msgCutoff())
	}
	return nil
}

func reserveSlot(ctx context.Context, tx pgx.Tx, batchID uuid.UUID, slot time.Time, qty int32) error {
	var qtyCap, ordCap *int32
	err := tx.QueryRow(ctx, `SELECT capacity_qty, capacity_orders FROM pickup_capacities WHERE batch_id = $1 AND slot_start = $2`, batchID, slot).Scan(&qtyCap, &ordCap)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var got int32
	err = tx.QueryRow(ctx, `
		UPDATE pickup_capacities
		SET reserved_qty = reserved_qty + $3,
		    reserved_orders = reserved_orders + 1
		WHERE batch_id = $1 AND slot_start = $2
		  AND (capacity_qty IS NULL OR reserved_qty + $3 <= capacity_qty)
		  AND (capacity_orders IS NULL OR reserved_orders + 1 <= capacity_orders)
		RETURNING reserved_qty
	`, batchID, slot, qty).Scan(&got)
	if errors.Is(err, pgx.ErrNoRows) {
		if qtyCap != nil {
			var used int32
			_ = tx.QueryRow(ctx, `SELECT reserved_qty FROM pickup_capacities WHERE batch_id = $1 AND slot_start = $2`, batchID, slot).Scan(&used)
			left := *qtyCap - used
			if left < 0 {
				left = 0
			}
			return E(409, "capacity", msgSlotQty(left))
		}
		return E(409, "capacity", msgSlotQty(0))
	}
	return err
}

func releaseOrderHold(ctx context.Context, q *db.Queries, tx pgx.Tx, o db.Order) (int, error) {
	if _, err := q.GetBatchForUpdate(ctx, o.BatchID); err != nil {
		return 0, err
	}
	if err := lockCapacityRows(ctx, tx, o.BatchID); err != nil {
		return 0, err
	}
	items, err := q.ListOrderItems(ctx, o.ID)
	if err != nil {
		return 0, err
	}
	var qty int32
	for _, it := range items {
		qty += it.Qty
		if err := q.ReleaseSoldCount(ctx, db.ReleaseSoldCountParams{ID: it.BatchItemID, SoldCount: it.Qty}); err != nil {
			return 0, err
		}
	}
	confirmed := o.PaymentStatus == "proof_submitted" || o.PaymentStatus == "paid"
	if _, err := tx.Exec(ctx, `
		UPDATE batch_capacities
		SET reserved_orders = GREATEST(0, reserved_orders - 1),
		    confirmed_orders = CASE WHEN $2 THEN GREATEST(0, confirmed_orders - 1) ELSE confirmed_orders END,
		    updated_at = now()
		WHERE batch_id = $1
	`, o.BatchID, confirmed); err != nil {
		return 0, err
	}
	if o.PickupSlotStart.Valid {
		if _, err := tx.Exec(ctx, `
			UPDATE pickup_capacities
			SET reserved_qty = GREATEST(0, reserved_qty - $3),
			    reserved_orders = GREATEST(0, reserved_orders - 1),
			    confirmed_qty = CASE WHEN $4 THEN GREATEST(0, confirmed_qty - $3) ELSE confirmed_qty END,
			    confirmed_orders = CASE WHEN $4 THEN GREATEST(0, confirmed_orders - 1) ELSE confirmed_orders END
			WHERE batch_id = $1 AND slot_start = $2
		`, o.BatchID, o.PickupSlotStart.Time, qty, confirmed); err != nil {
			return 0, err
		}
	}
	if err := syncFullness(ctx, q, tx, o.BatchID); err != nil {
		return 0, err
	}
	return promoteWaitlist(ctx, q, tx, o.BatchID)
}

func noteConfirmed(ctx context.Context, q *db.Queries, tx pgx.Tx, o db.Order, delta int) error {
	if delta == 0 {
		return nil
	}
	if _, err := q.GetBatchForUpdate(ctx, o.BatchID); err != nil {
		return err
	}
	if err := lockCapacityRows(ctx, tx, o.BatchID); err != nil {
		return err
	}
	items, err := q.ListOrderItems(ctx, o.ID)
	if err != nil {
		return err
	}
	var qty int32
	for _, it := range items {
		qty += it.Qty
	}
	step := int32(1)
	if delta < 0 {
		step = -1
	}
	if _, err := tx.Exec(ctx, `
		UPDATE batch_capacities
		SET confirmed_orders = GREATEST(0, confirmed_orders + $2), updated_at = now()
		WHERE batch_id = $1
	`, o.BatchID, step); err != nil {
		return err
	}
	if o.PickupSlotStart.Valid {
		_, err = tx.Exec(ctx, `
			UPDATE pickup_capacities
			SET confirmed_qty = GREATEST(0, confirmed_qty + $3),
			    confirmed_orders = GREATEST(0, confirmed_orders + $4)
			WHERE batch_id = $1 AND slot_start = $2
		`, o.BatchID, o.PickupSlotStart.Time, step*qty, step)
	}
	return err
}

func syncFullness(ctx context.Context, q *db.Queries, tx pgx.Tx, batchID uuid.UUID) error {
	b, err := q.GetBatch(ctx, batchID)
	if err != nil {
		return err
	}
	row, err := loadBatchCap(ctx, tx, batchID, false)
	if err != nil {
		return err
	}
	items, err := q.ListBatchItems(ctx, batchID)
	if err != nil {
		return err
	}
	caps, err := loadPickupCaps(ctx, tx, batchID)
	if err != nil {
		return err
	}
	full := batchFull(b, items, row, caps)
	if full && b.Status == "open" {
		if _, err := q.SetBatchStatus(ctx, db.SetBatchStatusParams{ID: batchID, Status: "closed"}); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE batch_capacities SET closed_reason = 'full', updated_at = now() WHERE batch_id = $1`, batchID)
		return err
	}
	if !full && b.Status == "closed" && row.ClosedReason == "full" && b.ClosesAt.After(time.Now()) && row.QuotaState != "cancelled" && row.QuotaState != "below_min" {
		if _, err := q.SetBatchStatus(ctx, db.SetBatchStatusParams{ID: batchID, Status: "open"}); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE batch_capacities SET closed_reason = '', updated_at = now() WHERE batch_id = $1`, batchID)
		return err
	}
	return nil
}

func batchFull(b db.PoBatch, items []db.BatchItem, row batchCapRow, caps []slotCap) bool {
	var held int32
	allLimited := len(items) > 0
	allGone := len(items) > 0
	for _, it := range items {
		held += it.SoldCount
		if it.Quota == nil {
			allLimited = false
			allGone = false
			continue
		}
		if it.SoldCount < *it.Quota {
			allGone = false
		}
	}
	if b.QuotaTotal != nil && held >= *b.QuotaTotal {
		return true
	}
	if allLimited && allGone {
		return true
	}
	if row.found && row.CapacityOrders != nil && row.ReservedOrders >= *row.CapacityOrders {
		return true
	}
	if len(caps) == 0 || len(ParseSlots(b.PickupSlots)) == 0 {
		return false
	}
	for _, s := range ParseSlots(b.PickupSlots) {
		c, ok := matchSlot(caps, s.Start)
		if !ok || !slotFull(c) {
			return false
		}
	}
	return true
}

func slotFull(c slotCap) bool {
	if c.CapacityQty == nil && c.CapacityOrders == nil {
		return false
	}
	if c.CapacityQty != nil && c.ReservedQty < *c.CapacityQty {
		return false
	}
	if c.CapacityOrders != nil && c.ReservedOrders < *c.CapacityOrders {
		return false
	}
	return true
}

func promoteWaitlist(ctx context.Context, q *db.Queries, tx pgx.Tx, batchID uuid.UUID) (int, error) {
	b, err := q.GetBatch(ctx, batchID)
	if err != nil {
		return 0, err
	}
	row, err := loadBatchCap(ctx, tx, batchID, false)
	if err != nil || !row.found || !row.WaitlistEnabled {
		return 0, err
	}
	if row.QuotaState == "cancelled" || row.QuotaState == "below_min" || !b.ClosesAt.After(time.Now()) {
		return 0, nil
	}
	if b.Status != "open" {
		return 0, nil
	}
	held, err := heldQty(ctx, tx, batchID)
	if err != nil {
		return 0, err
	}
	leftQty := remain(b.QuotaTotal, held)
	leftOrders := remain(row.CapacityOrders, row.ReservedOrders)
	caps, err := loadPickupCaps(ctx, tx, batchID)
	if err != nil {
		return 0, err
	}
	items, err := q.ListBatchItems(ctx, batchID)
	if err != nil {
		return 0, err
	}
	itemLeft := map[uuid.UUID]*int32{}
	for _, it := range items {
		itemLeft[it.ID] = remain(it.Quota, it.SoldCount)
	}
	rows, err := tx.Query(ctx, `
		SELECT id, batch_item_id, slot_start, qty, wa_number_e164
		FROM po_waitlist
		WHERE batch_id = $1 AND status = 'waiting'
		ORDER BY created_at
		FOR UPDATE
	`, batchID)
	if err != nil {
		return 0, err
	}
	type waiter struct {
		id   uuid.UUID
		item *uuid.UUID
		slot *time.Time
		qty  int32
		wa   string
	}
	var waiting []waiter
	for rows.Next() {
		var w waiter
		var item pgtype.UUID
		var slot pgtype.Timestamptz
		if err := rows.Scan(&w.id, &item, &slot, &w.qty, &w.wa); err != nil {
			rows.Close()
			return 0, err
		}
		if item.Valid {
			id := uuid.UUID(item.Bytes)
			w.item = &id
		}
		if slot.Valid {
			t := slot.Time
			w.slot = &t
		}
		waiting = append(waiting, w)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	slotLeftQty := map[int64]*int32{}
	slotLeftOrd := map[int64]*int32{}
	for i := range caps {
		key := caps[i].Start.UTC().UnixMicro()
		slotLeftQty[key] = remain(caps[i].CapacityQty, caps[i].ReservedQty)
		slotLeftOrd[key] = remain(caps[i].CapacityOrders, caps[i].ReservedOrders)
	}
	n := 0
	for _, w := range waiting {
		if n >= 20 {
			break
		}
		if leftOrders != nil && *leftOrders < 1 {
			break
		}
		if leftQty != nil && *leftQty < w.qty {
			continue
		}
		if w.item != nil {
			if rem, ok := itemLeft[*w.item]; ok && rem != nil && *rem < w.qty {
				continue
			}
		}
		var slotKey int64
		if w.slot != nil {
			slotKey = w.slot.UTC().UnixMicro()
			if rem := slotLeftQty[slotKey]; rem != nil && *rem < w.qty {
				continue
			}
			if rem := slotLeftOrd[slotKey]; rem != nil && *rem < 1 {
				continue
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE po_waitlist SET status = 'notified', notified_at = now() WHERE id = $1`, w.id); err != nil {
			return n, err
		}
		if err := insertNotice(ctx, tx, nil, batchID, w.wa, "waitlist_open", "Ada slot kosong", "Kapasitas "+b.Title+" longgar lagi. Yuk pesan sebelum penuh."); err != nil {
			return n, err
		}
		if leftQty != nil {
			*leftQty -= w.qty
		}
		if leftOrders != nil {
			*leftOrders--
		}
		if w.item != nil {
			if rem := itemLeft[*w.item]; rem != nil {
				*rem -= w.qty
			}
		}
		if w.slot != nil {
			if rem := slotLeftQty[slotKey]; rem != nil {
				*rem -= w.qty
			}
			if rem := slotLeftOrd[slotKey]; rem != nil {
				*rem--
			}
		}
		n++
	}
	return n, nil
}

func (a *App) pingWaitlist(ctx context.Context, brandID, batchID uuid.UUID, n int) {
	if n <= 0 {
		return
	}
	a.notifyBrandOwner(ctx, brandID, "waitlist_open", "Ada yang nunggu slot",
		fmt.Sprintf("%d orang di daftar tunggu sudah dikabari ada slot.", n),
		"/app/po/"+batchID.String(), "waitlist:"+batchID.String()+":"+uuid.NewString(), true)
}

func (a *App) reviewMinimums(ctx context.Context) {
	rows, err := a.Pool.Query(ctx, `
		SELECT b.id FROM po_batches b
		JOIN batch_capacities c ON c.batch_id = b.id
		WHERE c.min_proceed_qty IS NOT NULL
		  AND c.quota_state = 'ok'
		  AND b.closes_at <= now()
		  AND b.status <> 'completed'
	`)
	if err != nil {
		slog.Error("min quota", "err", err)
		return
	}
	defer rows.Close()
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			slog.Error("min quota", "err", err)
			return
		}
		ids = append(ids, id)
	}
	for _, id := range ids {
		msg, brand, ok, err := a.reviewOne(ctx, id)
		if err != nil {
			slog.Error("review minimum", "err", err, "batch", id)
			continue
		}
		if ok {
			a.notifyBrandOwner(ctx, brand, "po_below_min", "Kuota minimum belum tercapai", msg, "/app/po/"+id.String(), "below_min:"+id.String(), true)
		}
	}
}

func (a *App) reviewOne(ctx context.Context, batchID uuid.UUID) (string, uuid.UUID, bool, error) {
	var msg string
	var brand uuid.UUID
	var flagged bool
	err := a.txConn(ctx, func(q *db.Queries, tx pgx.Tx) error {
		b, err := q.GetBatchForUpdate(ctx, batchID)
		if err != nil {
			return err
		}
		if err := lockCapacityRows(ctx, tx, batchID); err != nil {
			return err
		}
		row, err := loadBatchCap(ctx, tx, batchID, false)
		if err != nil || !row.found || row.MinProceedQty == nil || row.QuotaState != "ok" || b.ClosesAt.After(time.Now()) {
			return err
		}
		brand = b.BrandID
		if row.ClosedReason == "" {
			if _, err := tx.Exec(ctx, `UPDATE batch_capacities SET closed_reason = 'schedule', updated_at = now() WHERE batch_id = $1`, batchID); err != nil {
				return err
			}
		}
		if b.Status == "open" {
			if _, err := q.SetBatchStatus(ctx, db.SetBatchStatusParams{ID: batchID, Status: "closed"}); err != nil {
				return err
			}
		}
		held, err := heldQty(ctx, tx, batchID)
		if err != nil {
			return err
		}
		if held >= *row.MinProceedQty {
			return nil
		}
		if _, err := tx.Exec(ctx, `
			UPDATE batch_capacities
			SET quota_state = 'below_min', notified_at = now(), updated_at = now()
			WHERE batch_id = $1 AND quota_state = 'ok'
		`, batchID); err != nil {
			return err
		}
		msg = fmt.Sprintf("Kuota minimum %s baru %d dari %d. Pilih lanjutkan atau batalkan.", b.Title, held, *row.MinProceedQty)
		orders, err := q.ListOrdersByBatch(ctx, batchID)
		if err != nil {
			return err
		}
		body := fmt.Sprintf("PO %s belum mencapai jumlah minimum. Penjual akan memilih lanjut atau batal. Kalau batal, pembayaran ditandai untuk dikembalikan.", b.Title)
		for _, o := range orders {
			if o.PaymentStatus == "cancelled" || o.PaymentStatus == "expired" {
				continue
			}
			cust, err := q.GetCustomer(ctx, o.CustomerID)
			wa := ""
			if err == nil {
				wa = cust.WaNumberE164
			}
			if err := insertNotice(ctx, tx, &o.ID, batchID, wa, "below_min", "Kuota minimum belum tercapai", body); err != nil {
				return err
			}
		}
		flagged = true
		return nil
	})
	return msg, brand, flagged, err
}

// DecideMinimum lets the seller proceed with a short batch or cancel it.
// Cancel marks paid and proof-submitted orders for a transfer refund.
func (a *App) DecideMinimum(ctx context.Context, actor, batchID uuid.UUID, proceed bool) error {
	cur, err := a.Q.GetBatch(ctx, batchID)
	if err != nil {
		return err
	}
	_, acc, err := a.OwnedBrand(ctx, actor, cur.BrandID)
	if err != nil {
		return err
	}
	var cancelled int
	err = a.txConn(ctx, func(q *db.Queries, tx pgx.Tx) error {
		b, err := q.GetBatchForUpdate(ctx, batchID)
		if err != nil {
			return err
		}
		if err := lockCapacityRows(ctx, tx, batchID); err != nil {
			return err
		}
		row, err := loadBatchCap(ctx, tx, batchID, false)
		if err != nil {
			return err
		}
		if !row.found || row.QuotaState != "below_min" {
			return E(409, "no_decision", "Belum ada keputusan kuota yang menunggu.")
		}
		if proceed {
			if _, err := tx.Exec(ctx, `
				UPDATE batch_capacities SET quota_state = 'proceed', decided_at = now(), updated_at = now() WHERE batch_id = $1
			`, batchID); err != nil {
				return err
			}
			orders, err := q.ListOrdersByBatch(ctx, batchID)
			if err != nil {
				return err
			}
			body := "PO " + b.Title + " tetap diproses meski jumlah minimum belum penuh."
			for _, o := range orders {
				if o.PaymentStatus == "cancelled" || o.PaymentStatus == "expired" {
					continue
				}
				if err := insertNotice(ctx, tx, &o.ID, batchID, "", "proceed", "PO tetap jalan", body); err != nil {
					return err
				}
			}
			a.audit(ctx, q, &acc.ID, &actor, "capacity.proceed", "batch", batchID.String(), nil, map[string]string{"state": "proceed"})
			return nil
		}
		if _, err := tx.Exec(ctx, `
			UPDATE batch_capacities SET quota_state = 'cancelled', decided_at = now(), updated_at = now() WHERE batch_id = $1
		`, batchID); err != nil {
			return err
		}
		if b.Status == "open" {
			if _, err := q.SetBatchStatus(ctx, db.SetBatchStatusParams{ID: batchID, Status: "closed"}); err != nil {
				return err
			}
		}
		orders, err := q.ListOrdersByBatch(ctx, batchID)
		if err != nil {
			return err
		}
		settings, err := a.LoadSettings(ctx)
		if err != nil {
			return err
		}
		now := time.Now()
		for _, o := range orders {
			if o.PaymentStatus == "cancelled" || o.PaymentStatus == "expired" {
				continue
			}
			o, err = q.GetOrderForUpdate(ctx, o.ID)
			if err != nil {
				return err
			}
			if o.PaymentStatus == "paid" && o.ChargedAt.Valid {
				w, err := q.GetWalletForAccountUpdate(ctx, acc.ID)
				if err != nil {
					return err
				}
				if _, _, err := applyLedger(ctx, q, w.ID, ledgerIn{
					Type: "refund_credit", Amount: settings.OrderFee, Key: "refund_credit:" + o.ID.String(),
					OrderID: &o.ID, BrandID: &o.BrandID, Actor: &actor, Reason: "Batal kuota " + o.Code,
				}); err != nil {
					return err
				}
				if err := q.AddCustomerSpend(ctx, db.AddCustomerSpendParams{ID: o.CustomerID, TotalSpent: -o.TotalDue}); err != nil {
					return err
				}
			}
			if _, err := releaseOrderHold(ctx, q, tx, o); err != nil {
				return err
			}
			if _, err := q.SetOrderPayment(ctx, db.SetOrderPaymentParams{
				ID: o.ID, PaymentStatus: "cancelled", PaidAt: o.PaidAt, ChargedAt: o.ChargedAt, CancelledAt: pgt(now),
			}); err != nil {
				return err
			}
			if o.PaymentStatus == "proof_submitted" || o.PaymentStatus == "paid" {
				if _, err := tx.Exec(ctx, `
					INSERT INTO order_refund_flags (order_id, batch_id, store_id, reason)
					VALUES ($1, $2, $3, $4)
					ON CONFLICT (order_id) DO NOTHING
				`, o.ID, b.ID, b.StoreID, "Kuota minimum tidak tercapai"); err != nil {
					return err
				}
				cust, _ := q.GetCustomer(ctx, o.CustomerID)
				wa := ""
				if cust.ID != uuid.Nil {
					wa = cust.WaNumberE164
				}
				if err := insertNotice(ctx, tx, &o.ID, b.ID, wa, "cancelled_refund", "Pesanan dibatalkan",
					"PO "+b.Title+" dibatalkan karena kuota minimum tidak tercapai. Pembayaran ditandai untuk dikembalikan."); err != nil {
					return err
				}
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO capacity_incidents (store_id, brand_id, batch_id, kind, qty)
				VALUES ($1, $2, $3, 'min_quota_cancel', 1)
			`, b.StoreID, b.BrandID, b.ID); err != nil {
				return err
			}
			cancelled++
		}
		a.audit(ctx, q, &acc.ID, &actor, "capacity.cancel", "batch", batchID.String(), nil, map[string]int{"orders": cancelled})
		return nil
	})
	if err != nil {
		return err
	}
	if proceed {
		a.notifyBrandOwner(ctx, cur.BrandID, "po_proceed", "PO tetap dilanjutkan", "Kamu memilih lanjut meski kuota minimum belum penuh.", "/app/po/"+batchID.String(), "proceed:"+batchID.String(), true)
	} else {
		a.notifyBrandOwner(ctx, cur.BrandID, "po_cancelled_min", "PO dibatalkan",
			fmt.Sprintf("%d pesanan dibatalkan. Yang sudah transfer ditandai untuk dikembalikan.", cancelled),
			"/app/po/"+batchID.String(), "cancel-min:"+batchID.String(), true)
	}
	return nil
}

func insertNotice(ctx context.Context, tx pgx.Tx, orderID *uuid.UUID, batchID uuid.UUID, wa, kind, title, body string) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO customer_notices (order_id, batch_id, wa_number_e164, kind, title, body)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, orderID, batchID, wa, kind, title, body)
	return err
}

func convertWaitlist(ctx context.Context, tx pgx.Tx, batchID uuid.UUID, wa string) {
	_, _ = tx.Exec(ctx, `
		UPDATE po_waitlist SET status = 'converted'
		WHERE batch_id = $1 AND wa_number_e164 = $2 AND status IN ('waiting', 'notified')
	`, batchID, wa)
}

// AdjustBatchCapacity raises or lowers a live PO. The floor is every order still holding a slot.
func (a *App) AdjustBatchCapacity(ctx context.Context, actor, batchID uuid.UUID, in BatchCapacityInput) (CapacityView, error) {
	cur, err := a.Q.GetBatch(ctx, batchID)
	if err != nil {
		return CapacityView{}, err
	}
	if _, _, err := a.OwnedBrand(ctx, actor, cur.BrandID); err != nil {
		return CapacityView{}, err
	}
	rule, err := a.normalizeCapacity(in.CapacityInput)
	if err != nil {
		return CapacityView{}, err
	}
	var notified int
	err = a.txConn(ctx, func(q *db.Queries, tx pgx.Tx) error {
		if _, err := q.GetBatchForUpdate(ctx, batchID); err != nil {
			return err
		}
		if err := lockCapacityRows(ctx, tx, batchID); err != nil {
			return err
		}
		held, err := heldQty(ctx, tx, batchID)
		if err != nil {
			return err
		}
		if rule.CapacityQty != nil && *rule.CapacityQty < held {
			return E(409, "capacity_floor", msgFloorQty(held))
		}
		reserved, confirmed, err := activeOrderCount(ctx, tx, batchID)
		if err != nil {
			return err
		}
		if rule.CapacityOrders != nil && *rule.CapacityOrders < reserved {
			return E(409, "capacity_floor", msgFloorOrders(reserved))
		}
		if err := saveBatchRule(ctx, tx, batchID, rule); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE batch_capacities
			SET reserved_orders = $2, confirmed_orders = $3, updated_at = now()
			WHERE batch_id = $1
		`, batchID, reserved, confirmed); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE po_batches SET quota_total = $2 WHERE id = $1`, batchID, rule.CapacityQty); err != nil {
			return err
		}
		for _, it := range in.Items {
			row, err := q.GetBatchItemForUpdate(ctx, it.BatchItemID)
			if err != nil {
				return err
			}
			if row.BatchID != batchID {
				return E(400, "bad_item", "Item bukan dari PO ini.")
			}
			if it.Quota != nil && *it.Quota < row.SoldCount {
				return E(409, "capacity_floor", fmt.Sprintf("Kuota %s tidak boleh di bawah yang sudah masuk (%d).", row.ProductName, row.SoldCount))
			}
			if it.MinQty != nil && *it.MinQty <= 0 || it.MaxPerCustomer != nil && *it.MaxPerCustomer <= 0 {
				return E(400, "bad_capacity", "Batas produk harus lebih dari 0.")
			}
			if _, err := tx.Exec(ctx, `UPDATE batch_items SET quota = $2 WHERE id = $1`, it.BatchItemID, it.Quota); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO batch_item_limits (batch_item_id, min_qty, max_qty_per_customer)
				VALUES ($1, $2, $3)
				ON CONFLICT (batch_item_id) DO UPDATE SET
					min_qty = EXCLUDED.min_qty,
					max_qty_per_customer = EXCLUDED.max_qty_per_customer
			`, it.BatchItemID, it.MinQty, it.MaxPerCustomer); err != nil {
				return err
			}
		}
		if err := replaceSlots(ctx, tx, batchID, in.Slots); err != nil {
			return err
		}
		if err := syncFullness(ctx, q, tx, batchID); err != nil {
			return err
		}
		n, err := promoteWaitlist(ctx, q, tx, batchID)
		notified = n
		return err
	})
	if err != nil {
		return CapacityView{}, err
	}
	if notified > 0 {
		a.pingWaitlist(ctx, cur.BrandID, batchID, notified)
	}
	return a.BatchCapacityView(ctx, batchID)
}

func replaceSlots(ctx context.Context, tx pgx.Tx, batchID uuid.UUID, slots []SlotCapacityInput) error {
	for _, s := range slots {
		if s.CapacityQty != nil && *s.CapacityQty < 0 || s.CapacityOrders != nil && *s.CapacityOrders < 0 {
			return E(400, "bad_capacity", "Kuota slot tidak boleh minus.")
		}
		var resQty, resOrd, confQty, confOrd int32
		err := tx.QueryRow(ctx, `
			SELECT COALESCE(SUM(oi.qty), 0)::int,
			       COUNT(DISTINCT o.id)::int,
			       COALESCE(SUM(oi.qty) FILTER (WHERE o.payment_status IN ('proof_submitted', 'paid')), 0)::int,
			       COUNT(DISTINCT o.id) FILTER (WHERE o.payment_status IN ('proof_submitted', 'paid'))::int
			FROM orders o
			JOIN order_items oi ON oi.order_id = o.id
			WHERE o.batch_id = $1
			  AND o.payment_status NOT IN ('cancelled', 'expired')
			  AND o.pickup_slot_start = $2
		`, batchID, s.Start).Scan(&resQty, &resOrd, &confQty, &confOrd)
		if err != nil {
			return err
		}
		if s.CapacityQty != nil && *s.CapacityQty < resQty {
			return E(409, "capacity_floor", msgFloorQty(resQty))
		}
		if s.CapacityOrders != nil && *s.CapacityOrders < resOrd {
			return E(409, "capacity_floor", msgFloorOrders(resOrd))
		}
		if s.CapacityQty == nil && s.CapacityOrders == nil {
			if resQty == 0 && resOrd == 0 {
				_, err = tx.Exec(ctx, `DELETE FROM pickup_capacities WHERE batch_id = $1 AND slot_start = $2`, batchID, s.Start)
			} else {
				_, err = tx.Exec(ctx, `
					UPDATE pickup_capacities SET capacity_qty = NULL, capacity_orders = NULL
					WHERE batch_id = $1 AND slot_start = $2
				`, batchID, s.Start)
			}
			if err != nil {
				return err
			}
			continue
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO pickup_capacities (
				batch_id, slot_start, capacity_qty, capacity_orders,
				reserved_qty, reserved_orders, confirmed_qty, confirmed_orders
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			ON CONFLICT (batch_id, slot_start) DO UPDATE SET
				capacity_qty = EXCLUDED.capacity_qty,
				capacity_orders = EXCLUDED.capacity_orders,
				reserved_qty = EXCLUDED.reserved_qty,
				reserved_orders = EXCLUDED.reserved_orders,
				confirmed_qty = EXCLUDED.confirmed_qty,
				confirmed_orders = EXCLUDED.confirmed_orders
		`, batchID, s.Start, s.CapacityQty, s.CapacityOrders, resQty, resOrd, confQty, confOrd); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) JoinWaitlist(ctx context.Context, code string, in WaitlistIn) (WaitlistOut, error) {
	wa, err := text.WhatsApp(in.WhatsApp)
	if err != nil || in.Name == "" {
		return WaitlistOut{}, E(400, "bad_customer", "Isi nama dan nomor WhatsApp yang aktif ya.")
	}
	if in.Qty <= 0 {
		return WaitlistOut{}, E(400, "bad_qty", "Jumlah pesanan belum pas.")
	}
	b, err := a.Q.GetBatchByCode(ctx, code)
	if errors.Is(err, pgx.ErrNoRows) {
		return WaitlistOut{}, E(404, "not_found", "PO tidak ketemu.")
	}
	if err != nil {
		return WaitlistOut{}, err
	}
	view, err := a.viewFor(ctx, a.Pool, b)
	if err != nil {
		return WaitlistOut{}, err
	}
	if !view.WaitlistEnabled {
		return WaitlistOut{}, E(400, "waitlist_off", "Daftar tunggu tidak dibuka untuk PO ini.")
	}
	if view.QuotaState == "cancelled" {
		return WaitlistOut{}, E(409, "batch_closed", "PO ini dibatalkan.")
	}
	if !b.ClosesAt.After(time.Now()) || (b.Status != "open" && view.ClosedReason != "full") {
		return WaitlistOut{}, E(409, "batch_closed", "PO ini sudah ditutup.")
	}
	var slot *time.Time
	slots := ParseSlots(b.PickupSlots)
	if len(slots) > 0 {
		if in.SlotIndex < 0 || in.SlotIndex >= len(slots) {
			return WaitlistOut{}, E(400, "bad_slot", "Pilih slot ambil yang tersedia.")
		}
		t := slots[in.SlotIndex].Start
		slot = &t
	}
	if in.BatchItemID != nil {
		items, err := a.Q.ListBatchItems(ctx, b.ID)
		if err != nil {
			return WaitlistOut{}, err
		}
		ok := false
		for _, it := range items {
			if it.ID == *in.BatchItemID {
				ok = true
				break
			}
		}
		if !ok {
			return WaitlistOut{}, E(400, "bad_item", "Ada item yang bukan dari PO ini.")
		}
	}
	if roomFor(view, in.Qty, in.BatchItemID, slot) && b.Status == "open" {
		return WaitlistOut{}, E(409, "still_open", "Masih ada slot, langsung pesan ya.")
	}
	if view.MaxQtyPerCustomer != nil && in.Qty > *view.MaxQtyPerCustomer {
		return WaitlistOut{}, E(400, "above_max", msgMax(*view.MaxQtyPerCustomer))
	}
	var existing string
	err = a.Pool.QueryRow(ctx, `
		SELECT public_token FROM po_waitlist
		WHERE batch_id = $1 AND wa_number_e164 = $2 AND status = 'waiting'
	`, b.ID, wa).Scan(&existing)
	if err == nil {
		return WaitlistOut{Token: existing, Status: "waiting", Message: "Kamu sudah di daftar tunggu. Kami kabari kalau ada slot."}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return WaitlistOut{}, err
	}
	token, err := publicToken()
	if err != nil {
		return WaitlistOut{}, err
	}
	_, err = a.Pool.Exec(ctx, `
		INSERT INTO po_waitlist (batch_id, batch_item_id, slot_start, name, wa_number_e164, qty, public_token)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, b.ID, in.BatchItemID, slot, in.Name, wa, in.Qty, token)
	if err != nil {
		return WaitlistOut{}, err
	}
	return WaitlistOut{Token: token, Status: "waiting", Message: "Masuk daftar tunggu. Kami kabari kalau ada slot."}, nil
}

func roomFor(view CapacityView, qty int32, item *uuid.UUID, slot *time.Time) bool {
	if view.RemainingQty != nil && *view.RemainingQty < qty {
		return false
	}
	if view.RemainingOrders != nil && *view.RemainingOrders < 1 {
		return false
	}
	if item != nil {
		if lim, ok := view.Items[*item]; ok && lim.Remaining != nil && *lim.Remaining < qty {
			return false
		}
	}
	if slot != nil {
		for _, s := range view.Slots {
			if s.Start.UTC().Truncate(time.Microsecond).Equal(slot.UTC().Truncate(time.Microsecond)) {
				if s.RemainingQty != nil && *s.RemainingQty < qty {
					return false
				}
				if s.RemainingOrders != nil && *s.RemainingOrders < 1 {
					return false
				}
			}
		}
	}
	return true
}

func (a *App) WaitlistStatus(ctx context.Context, token string) (WaitlistOut, error) {
	var status, name string
	var batch uuid.UUID
	err := a.Pool.QueryRow(ctx, `SELECT status, name, batch_id FROM po_waitlist WHERE public_token = $1`, token).Scan(&status, &name, &batch)
	if errors.Is(err, pgx.ErrNoRows) {
		return WaitlistOut{}, E(404, "not_found", "Daftar tunggu tidak ketemu.")
	}
	if err != nil {
		return WaitlistOut{}, err
	}
	msg := "Kamu masih di daftar tunggu."
	switch status {
	case "notified":
		msg = "Ada slot kosong. Yuk pesan sebelum penuh lagi."
	case "converted":
		msg = "Pesananmu sudah masuk. Cek status bayarnya ya."
	case "cancelled":
		msg = "Antrian ini sudah ditutup."
	}
	_ = name
	_ = batch
	return WaitlistOut{Token: token, Status: status, Message: msg}, nil
}

// PreviewProblems is the cart's early copy of the checkout checks. It does not reserve.
func (a *App) PreviewProblems(ctx context.Context, b db.PoBatch, lines []CartItem, sub int64, wa string, slot *time.Time) []string {
	var problems []string
	row, err := loadBatchCap(ctx, a.Pool, b.ID, false)
	if err != nil {
		return nil
	}
	add := lineQty(lines)
	if !b.ClosesAt.After(time.Now()) || b.Status != "open" {
		problems = append(problems, "PO ini sudah ditutup.")
	}
	if row.found {
		if err := rejectCutoff(row.capRule, b, slot); err != nil {
			problems = append(problems, err.Error())
		}
		if row.MinQty != nil && add < *row.MinQty {
			problems = append(problems, msgMinQty(*row.MinQty))
		}
		if row.MinAmountRp != nil && sub < *row.MinAmountRp {
			problems = append(problems, msgMinRp(*row.MinAmountRp))
		}
	}
	if b.QuotaTotal != nil {
		held, err := heldQty(ctx, a.Pool, b.ID)
		if err == nil && held+add > *b.QuotaTotal {
			left := *b.QuotaTotal - held
			if left < 0 {
				left = 0
			}
			problems = append(problems, msgBatchLeft(left))
		}
	}
	if row.found && row.CapacityOrders != nil && row.ReservedOrders+1 > *row.CapacityOrders {
		problems = append(problems, msgOrdersFull())
	}
	if wa != "" && row.found && row.MaxQtyPerCustomer != nil {
		if number, err := text.WhatsApp(wa); err == nil {
			var custID uuid.UUID
			err = a.Pool.QueryRow(ctx, `SELECT id FROM customers WHERE brand_id = $1 AND wa_number_e164 = $2`, b.BrandID, number).Scan(&custID)
			if err == nil {
				var have int32
				_ = a.Pool.QueryRow(ctx, `
					SELECT COALESCE(SUM(oi.qty), 0)::int
					FROM order_items oi JOIN orders o ON o.id = oi.order_id
					WHERE o.batch_id = $1 AND o.customer_id = $2
					  AND o.payment_status NOT IN ('cancelled', 'expired')
				`, b.ID, custID).Scan(&have)
				if have+add > *row.MaxQtyPerCustomer {
					problems = append(problems, msgMax(*row.MaxQtyPerCustomer))
				}
			}
		}
	}
	return problems
}
