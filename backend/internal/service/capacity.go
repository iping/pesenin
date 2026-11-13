package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"pesenin.product.biz.id/api/internal/db"
	"pesenin.product.biz.id/api/internal/text"
)

// Capacity is resolved in layers: brand default, then store override, then the batch snapshot.
// Store opening hours are not read here. PlaceOrder keeps that check where it already lives.

type qdb interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

type capRule struct {
	MinQty            *int32
	MinAmountRp       *int64
	MaxQtyPerCustomer *int32
	CapacityQty       *int32
	CapacityOrders    *int32
	MinProceedQty     *int32
	CutoffLeadMinutes *int32
	WaitlistEnabled   bool
}

type storedRule struct {
	capRule
	waitlist *bool
	found    bool
}

type batchCapRow struct {
	capRule
	ReservedOrders  int32
	ConfirmedOrders int32
	ClosedReason    string
	QuotaState      string
	found           bool
}

type itemLimit struct {
	MinQty         *int32
	MaxPerCustomer *int32
}

type slotCap struct {
	Start           time.Time
	CapacityQty     *int32
	CapacityOrders  *int32
	ReservedQty     int32
	ReservedOrders  int32
	ConfirmedQty    int32
	ConfirmedOrders int32
}

// CapacityInput is the seller form for a brand default, a store override, or a batch snapshot.
type CapacityInput struct {
	MinQty            *int32 `json:"minQty"`
	MinAmountRp       *int64 `json:"minAmountRp"`
	MaxQtyPerCustomer *int32 `json:"maxQtyPerCustomer"`
	CapacityQty       *int32 `json:"capacityQty"`
	CapacityOrders    *int32 `json:"capacityOrders"`
	MinProceedQty     *int32 `json:"minProceedQty"`
	CutoffLeadMinutes *int32 `json:"cutoffLeadMinutes"`
	WaitlistEnabled   bool   `json:"waitlistEnabled"`
}

func (in CapacityInput) rule() capRule {
	return capRule{
		MinQty: in.MinQty, MinAmountRp: in.MinAmountRp, MaxQtyPerCustomer: in.MaxQtyPerCustomer,
		CapacityQty: in.CapacityQty, CapacityOrders: in.CapacityOrders, MinProceedQty: in.MinProceedQty,
		CutoffLeadMinutes: in.CutoffLeadMinutes, WaitlistEnabled: in.WaitlistEnabled,
	}
}

func ruleInput(r capRule) CapacityInput {
	return CapacityInput{
		MinQty: r.MinQty, MinAmountRp: r.MinAmountRp, MaxQtyPerCustomer: r.MaxQtyPerCustomer,
		CapacityQty: r.CapacityQty, CapacityOrders: r.CapacityOrders, MinProceedQty: r.MinProceedQty,
		CutoffLeadMinutes: r.CutoffLeadMinutes, WaitlistEnabled: r.WaitlistEnabled,
	}
}

// ProductCapacityInput is the default applied when a product is added to a batch.
type ProductCapacityInput struct {
	MinQty         *int32 `json:"minQty"`
	MaxPerCustomer *int32 `json:"maxPerCustomer"`
	CapacityQty    *int32 `json:"capacityQty"`
}

// ItemCapacityInput edits one batch line while the PO is open.
type ItemCapacityInput struct {
	BatchItemID    uuid.UUID `json:"batchItemId"`
	Quota          *int32    `json:"quota"`
	MinQty         *int32    `json:"minQty"`
	MaxPerCustomer *int32    `json:"maxPerCustomer"`
}

// SlotCapacityInput edits one pickup window.
type SlotCapacityInput struct {
	Start          time.Time `json:"start"`
	CapacityQty    *int32    `json:"capacityQty"`
	CapacityOrders *int32    `json:"capacityOrders"`
}

// BatchCapacityInput replaces the batch snapshot. Null limits mean unlimited.
type BatchCapacityInput struct {
	CapacityInput
	Items []ItemCapacityInput `json:"items"`
	Slots []SlotCapacityInput `json:"slots"`
}

// CapacityView is what the shop and the seller PO screen share.
type CapacityView struct {
	MinQty            *int32                `json:"minQty"`
	MinAmountRp       *int64                `json:"minAmountRp"`
	MaxQtyPerCustomer *int32                `json:"maxQtyPerCustomer"`
	CapacityOrders    *int32                `json:"capacityOrders"`
	ReservedOrders    int32                 `json:"reservedOrders"`
	ConfirmedOrders   int32                 `json:"confirmedOrders"`
	RemainingOrders   *int32                `json:"remainingOrders"`
	MinProceedQty     *int32                `json:"minProceedQty"`
	HeldQty           int32                 `json:"heldQty"`
	RemainingQty      *int32                `json:"remainingQty"`
	CutoffLeadMinutes *int32                `json:"cutoffLeadMinutes"`
	WaitlistEnabled   bool                  `json:"waitlistEnabled"`
	QuotaState        string                `json:"quotaState"`
	ClosedReason      string                `json:"closedReason"`
	Items             map[uuid.UUID]ItemCap `json:"-"`
	Slots             []SlotCap             `json:"slots"`
}

// ItemCap is the per-product limit merged onto a batch line.
type ItemCap struct {
	MinQty         *int32 `json:"minQty"`
	MaxPerCustomer *int32 `json:"maxPerCustomer"`
	Remaining      *int32 `json:"remaining"`
}

// SlotCap is remaining capacity for one pickup window.
type SlotCap struct {
	Start           time.Time `json:"start"`
	CapacityQty     *int32    `json:"capacityQty"`
	RemainingQty    *int32    `json:"remainingQty"`
	CapacityOrders  *int32    `json:"capacityOrders"`
	RemainingOrders *int32    `json:"remainingOrders"`
}

// CustomerNotice is shown on the guest order page. Guests have no push subscription.
type CustomerNotice struct {
	Kind  string `json:"kind"`
	Title string `json:"title"`
	Body  string `json:"body"`
}

// StoreCapacityStat is the Office Pesenin per-store rollup.
type StoreCapacityStat struct {
	StoreID         uuid.UUID `json:"storeId"`
	StoreName       string    `json:"storeName"`
	BrandName       string    `json:"brandName"`
	OversellBlocked int32     `json:"oversellBlocked"`
	Cancellations   int32     `json:"cancellations"`
	OversoldNow     int32     `json:"oversoldNow"`
}

// StoreCapacitySetting is the brand default, the override, and the resolved rule.
type StoreCapacitySetting struct {
	StoreID   uuid.UUID      `json:"storeId"`
	Name      string         `json:"name"`
	Inherit   bool           `json:"inherit"`
	Effective CapacityInput  `json:"effective"`
	Override  *CapacityInput `json:"override"`
}

// ProductCapacitySetting is the catalog default for one product.
type ProductCapacitySetting struct {
	ProductID      uuid.UUID `json:"productId"`
	Name           string    `json:"name"`
	MinQty         *int32    `json:"minQty"`
	MaxPerCustomer *int32    `json:"maxPerCustomer"`
	CapacityQty    *int32    `json:"capacityQty"`
}

// CapacitySettings is the seller screen for defaults.
type CapacitySettings struct {
	Brand    CapacityInput            `json:"brand"`
	Stores   []StoreCapacitySetting   `json:"stores"`
	Products []ProductCapacitySetting `json:"products"`
}

// WaitlistIn joins the optional list for a full PO.
type WaitlistIn struct {
	Name        string
	WhatsApp    string
	Qty         int32
	BatchItemID *uuid.UUID
	SlotIndex   int
}

// WaitlistOut is safe to show to the guest who holds the token.
type WaitlistOut struct {
	Token   string `json:"token"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

func msgMinQty(n int32) string {
	return fmt.Sprintf("Minimal %d pcs untuk pesanan ini.", n)
}
func msgMinRp(n int64) string {
	return "Minimal belanja Rp " + text.Rupiah(n) + " untuk pesanan ini."
}
func msgMax(n int32) string { return fmt.Sprintf("Maksimal %d pcs per pelanggan untuk PO ini.", n) }
func msgMaxItem(name string, n int32) string {
	return fmt.Sprintf("%s maksimal %d pcs per pelanggan.", name, n)
}
func msgOrdersFull() string { return "Kuota pesanan PO ini sudah penuh." }
func msgSlotQty(n int32) string {
	if n <= 0 {
		return "Slot ambil ini sudah penuh."
	}
	return fmt.Sprintf("Slot ambil ini sisa %d.", n)
}
func msgCutoff() string {
	return "Pemesanan untuk jadwal ini sudah ditutup. Batas pesan dihitung dari waktu ambil."
}
func msgFloorQty(n int32) string {
	return fmt.Sprintf("Kapasitas tidak boleh di bawah pesanan yang sudah masuk (%d pcs).", n)
}
func msgFloorOrders(n int32) string {
	return fmt.Sprintf("Kapasitas pesanan tidak boleh di bawah pesanan yang sudah masuk (%d).", n)
}
func msgBatchLeft(n int32) string { return fmt.Sprintf("Kuota PO ini sisa %d pcs.", n) }

func validateRule(r capRule) error {
	if r.MinQty != nil && *r.MinQty <= 0 {
		return E(400, "bad_capacity", "Minimal pcs harus lebih dari 0.")
	}
	if r.MinAmountRp != nil && *r.MinAmountRp <= 0 {
		return E(400, "bad_capacity", "Minimal belanja harus lebih dari 0.")
	}
	if r.MaxQtyPerCustomer != nil && *r.MaxQtyPerCustomer <= 0 {
		return E(400, "bad_capacity", "Maksimal per pelanggan harus lebih dari 0.")
	}
	if r.CapacityQty != nil && *r.CapacityQty < 0 {
		return E(400, "bad_capacity", "Kuota tidak boleh minus.")
	}
	if r.CapacityOrders != nil && *r.CapacityOrders < 0 {
		return E(400, "bad_capacity", "Kuota pesanan tidak boleh minus.")
	}
	if r.MinProceedQty != nil && *r.MinProceedQty <= 0 {
		return E(400, "bad_capacity", "Kuota minimum lanjut harus lebih dari 0.")
	}
	if r.CutoffLeadMinutes != nil && *r.CutoffLeadMinutes < 0 {
		return E(400, "bad_capacity", "Batas waktu pesan tidak boleh minus.")
	}
	return nil
}

func (a *App) normalizeCapacity(in CapacityInput) (capRule, error) {
	r := in.rule()
	return r, validateRule(r)
}

const ruleCols = `min_qty, min_amount_rp, max_qty_per_customer, capacity_qty, capacity_orders, min_proceed_qty, cutoff_lead_minutes, waitlist_enabled`

func scanStored(row pgx.Row) (storedRule, error) {
	var s storedRule
	err := row.Scan(&s.MinQty, &s.MinAmountRp, &s.MaxQtyPerCustomer, &s.CapacityQty, &s.CapacityOrders, &s.MinProceedQty, &s.CutoffLeadMinutes, &s.waitlist)
	if errors.Is(err, pgx.ErrNoRows) {
		return storedRule{}, nil
	}
	if err != nil {
		return storedRule{}, err
	}
	s.found = true
	if s.waitlist != nil {
		s.WaitlistEnabled = *s.waitlist
	}
	return s, nil
}

func loadBrandStored(ctx context.Context, qx qdb, brandID uuid.UUID) (storedRule, error) {
	return scanStored(qx.QueryRow(ctx, `SELECT `+ruleCols+` FROM po_capacity_rules WHERE brand_id = $1 AND store_id IS NULL`, brandID))
}

func loadStoreStored(ctx context.Context, qx qdb, storeID uuid.UUID) (storedRule, error) {
	return scanStored(qx.QueryRow(ctx, `SELECT `+ruleCols+` FROM po_capacity_rules WHERE store_id = $1`, storeID))
}

func overlay(base capRule, over storedRule) capRule {
	if !over.found {
		return base
	}
	if over.MinQty != nil {
		base.MinQty = over.MinQty
	}
	if over.MinAmountRp != nil {
		base.MinAmountRp = over.MinAmountRp
	}
	if over.MaxQtyPerCustomer != nil {
		base.MaxQtyPerCustomer = over.MaxQtyPerCustomer
	}
	if over.CapacityQty != nil {
		base.CapacityQty = over.CapacityQty
	}
	if over.CapacityOrders != nil {
		base.CapacityOrders = over.CapacityOrders
	}
	if over.MinProceedQty != nil {
		base.MinProceedQty = over.MinProceedQty
	}
	if over.CutoffLeadMinutes != nil {
		base.CutoffLeadMinutes = over.CutoffLeadMinutes
	}
	if over.waitlist != nil {
		base.WaitlistEnabled = *over.waitlist
	}
	return base
}

func applyExplicit(r *capRule, in BatchIn) {
	if in.MinQty != nil {
		r.MinQty = in.MinQty
	}
	if in.MinAmountRp != nil {
		r.MinAmountRp = in.MinAmountRp
	}
	if in.MaxQtyPerCustomer != nil {
		r.MaxQtyPerCustomer = in.MaxQtyPerCustomer
	}
	if in.CapacityOrders != nil {
		r.CapacityOrders = in.CapacityOrders
	}
	if in.MinProceedQty != nil {
		r.MinProceedQty = in.MinProceedQty
	}
	if in.CutoffLeadMinutes != nil {
		r.CutoffLeadMinutes = in.CutoffLeadMinutes
	}
	if in.WaitlistEnabled != nil {
		r.WaitlistEnabled = *in.WaitlistEnabled
	}
	if in.QuotaTotal != nil {
		r.CapacityQty = in.QuotaTotal
	}
}

func resolveNewBatch(ctx context.Context, tx pgx.Tx, brandID, storeID uuid.UUID, in BatchIn) (capRule, error) {
	if in.Snapshot != nil {
		r := *in.Snapshot
		applyExplicit(&r, in)
		return r, validateRule(r)
	}
	if in.RulesSet {
		var r capRule
		applyExplicit(&r, in)
		return r, validateRule(r)
	}
	brand, err := loadBrandStored(ctx, tx, brandID)
	if err != nil {
		return capRule{}, err
	}
	rule := brand.capRule
	store, err := loadStoreStored(ctx, tx, storeID)
	if err != nil {
		return capRule{}, err
	}
	rule = overlay(rule, store)
	applyExplicit(&rule, in)
	return rule, validateRule(rule)
}

func saveBatchRule(ctx context.Context, tx pgx.Tx, batchID uuid.UUID, r capRule) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO batch_capacities (
			batch_id, min_qty, min_amount_rp, max_qty_per_customer, capacity_orders,
			min_proceed_qty, cutoff_lead_minutes, waitlist_enabled
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (batch_id) DO UPDATE SET
			min_qty = EXCLUDED.min_qty,
			min_amount_rp = EXCLUDED.min_amount_rp,
			max_qty_per_customer = EXCLUDED.max_qty_per_customer,
			capacity_orders = EXCLUDED.capacity_orders,
			min_proceed_qty = EXCLUDED.min_proceed_qty,
			cutoff_lead_minutes = EXCLUDED.cutoff_lead_minutes,
			waitlist_enabled = EXCLUDED.waitlist_enabled,
			updated_at = now()
	`, batchID, r.MinQty, r.MinAmountRp, r.MaxQtyPerCustomer, r.CapacityOrders, r.MinProceedQty, r.CutoffLeadMinutes, r.WaitlistEnabled)
	return err
}

func savePickupCaps(ctx context.Context, tx pgx.Tx, batchID uuid.UUID, slots []Slot) error {
	for _, s := range slots {
		if s.CapacityQty == nil && s.CapacityOrders == nil {
			continue
		}
		if s.CapacityQty != nil && *s.CapacityQty < 0 || s.CapacityOrders != nil && *s.CapacityOrders < 0 {
			return E(400, "bad_capacity", "Kuota slot tidak boleh minus.")
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO pickup_capacities (batch_id, slot_start, capacity_qty, capacity_orders)
			VALUES ($1, $2, $3, $4)
		`, batchID, s.Start, s.CapacityQty, s.CapacityOrders); err != nil {
			return err
		}
	}
	return nil
}

func loadBatchCap(ctx context.Context, qx qdb, batchID uuid.UUID, lock bool) (batchCapRow, error) {
	q := `SELECT min_qty, min_amount_rp, max_qty_per_customer, capacity_orders,
		reserved_orders, confirmed_orders, min_proceed_qty, cutoff_lead_minutes,
		waitlist_enabled, closed_reason, quota_state
		FROM batch_capacities WHERE batch_id = $1`
	if lock {
		q += ` FOR UPDATE`
	}
	var row batchCapRow
	err := qx.QueryRow(ctx, q, batchID).Scan(
		&row.MinQty, &row.MinAmountRp, &row.MaxQtyPerCustomer, &row.CapacityOrders,
		&row.ReservedOrders, &row.ConfirmedOrders, &row.MinProceedQty, &row.CutoffLeadMinutes,
		&row.WaitlistEnabled, &row.ClosedReason, &row.QuotaState,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return batchCapRow{QuotaState: "ok"}, nil
	}
	if err != nil {
		return batchCapRow{}, err
	}
	row.found = true
	return row, nil
}

func (a *App) loadBatchRule(ctx context.Context, qx qdb, batchID uuid.UUID) (capRule, bool, error) {
	row, err := loadBatchCap(ctx, qx, batchID, false)
	if err != nil {
		return capRule{}, false, err
	}
	return row.capRule, row.found, nil
}

func loadPickupCaps(ctx context.Context, qx qdb, batchID uuid.UUID) ([]slotCap, error) {
	rows, err := qx.Query(ctx, `
		SELECT slot_start, capacity_qty, capacity_orders, reserved_qty, reserved_orders, confirmed_qty, confirmed_orders
		FROM pickup_capacities WHERE batch_id = $1 ORDER BY slot_start
	`, batchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []slotCap
	for rows.Next() {
		var s slotCap
		if err := rows.Scan(&s.Start, &s.CapacityQty, &s.CapacityOrders, &s.ReservedQty, &s.ReservedOrders, &s.ConfirmedQty, &s.ConfirmedOrders); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func matchSlot(caps []slotCap, start time.Time) (slotCap, bool) {
	for _, c := range caps {
		if c.Start.UTC().Truncate(time.Microsecond).Equal(start.UTC().Truncate(time.Microsecond)) {
			return c, true
		}
	}
	return slotCap{}, false
}

func loadItemLimits(ctx context.Context, qx qdb, batchID uuid.UUID) (map[uuid.UUID]itemLimit, error) {
	rows, err := qx.Query(ctx, `
		SELECT l.batch_item_id, l.min_qty, l.max_qty_per_customer
		FROM batch_item_limits l
		JOIN batch_items bi ON bi.id = l.batch_item_id
		WHERE bi.batch_id = $1
	`, batchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uuid.UUID]itemLimit{}
	for rows.Next() {
		var id uuid.UUID
		var lim itemLimit
		if err := rows.Scan(&id, &lim.MinQty, &lim.MaxPerCustomer); err != nil {
			return nil, err
		}
		out[id] = lim
	}
	return out, rows.Err()
}

func productRuleQuota(ctx context.Context, qx qdb, productID uuid.UUID) (*int32, itemLimit, error) {
	var lim itemLimit
	var cap *int32
	err := qx.QueryRow(ctx, `SELECT min_qty, max_qty_per_customer, capacity_qty FROM product_capacity_rules WHERE product_id = $1`, productID).Scan(&lim.MinQty, &lim.MaxPerCustomer, &cap)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, itemLimit{}, nil
	}
	if err != nil {
		return nil, itemLimit{}, err
	}
	return cap, lim, nil
}

func heldQty(ctx context.Context, qx qdb, batchID uuid.UUID) (int32, error) {
	var n int32
	err := qx.QueryRow(ctx, `SELECT COALESCE(SUM(sold_count), 0)::int FROM batch_items WHERE batch_id = $1`, batchID).Scan(&n)
	return n, err
}

func activeOrderCount(ctx context.Context, qx qdb, batchID uuid.UUID) (reserved, confirmed int32, err error) {
	err = qx.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE payment_status NOT IN ('cancelled', 'expired'))::int,
		       count(*) FILTER (WHERE payment_status IN ('proof_submitted', 'paid'))::int
		FROM orders WHERE batch_id = $1
	`, batchID).Scan(&reserved, &confirmed)
	return reserved, confirmed, err
}

func guardQtyFloor(ctx context.Context, tx pgx.Tx, batchID uuid.UUID, quota *int32) error {
	if quota == nil {
		return nil
	}
	held, err := heldQty(ctx, tx, batchID)
	if err != nil {
		return err
	}
	if *quota < held {
		return E(409, "capacity_floor", msgFloorQty(held))
	}
	return nil
}

func (a *App) markClosedReason(ctx context.Context, batchID uuid.UUID, reason string) {
	if _, err := a.Pool.Exec(ctx, `UPDATE batch_capacities SET closed_reason = $2, updated_at = now() WHERE batch_id = $1`, batchID, reason); err != nil {
		slog.Error("closed reason", "err", err, "batch", batchID)
	}
}

func remain(cap *int32, used int32) *int32 {
	if cap == nil {
		return nil
	}
	r := *cap - used
	if r < 0 {
		r = 0
	}
	return &r
}

func (a *App) BatchCapacityView(ctx context.Context, batchID uuid.UUID) (CapacityView, error) {
	b, err := a.Q.GetBatch(ctx, batchID)
	if err != nil {
		return CapacityView{}, err
	}
	return a.viewFor(ctx, a.Pool, b)
}

func (a *App) viewFor(ctx context.Context, qx qdb, b db.PoBatch) (CapacityView, error) {
	row, err := loadBatchCap(ctx, qx, b.ID, false)
	if err != nil {
		return CapacityView{}, err
	}
	held, err := heldQty(ctx, qx, b.ID)
	if err != nil {
		return CapacityView{}, err
	}
	caps, err := loadPickupCaps(ctx, qx, b.ID)
	if err != nil {
		return CapacityView{}, err
	}
	limits, err := loadItemLimits(ctx, qx, b.ID)
	if err != nil {
		return CapacityView{}, err
	}
	items, err := a.Q.ListBatchItems(ctx, b.ID)
	if err != nil {
		return CapacityView{}, err
	}
	row.CapacityQty = b.QuotaTotal
	view := CapacityView{
		MinQty: row.MinQty, MinAmountRp: row.MinAmountRp, MaxQtyPerCustomer: row.MaxQtyPerCustomer,
		CapacityOrders: row.CapacityOrders, ReservedOrders: row.ReservedOrders, ConfirmedOrders: row.ConfirmedOrders,
		RemainingOrders: remain(row.CapacityOrders, row.ReservedOrders), MinProceedQty: row.MinProceedQty,
		HeldQty: held, RemainingQty: remain(b.QuotaTotal, held), CutoffLeadMinutes: row.CutoffLeadMinutes,
		WaitlistEnabled: row.WaitlistEnabled, QuotaState: row.QuotaState, ClosedReason: row.ClosedReason,
		Items: map[uuid.UUID]ItemCap{},
	}
	if view.QuotaState == "" {
		view.QuotaState = "ok"
	}
	for _, it := range items {
		lim := limits[it.ID]
		view.Items[it.ID] = ItemCap{MinQty: lim.MinQty, MaxPerCustomer: lim.MaxPerCustomer, Remaining: remain(it.Quota, it.SoldCount)}
	}
	for _, s := range caps {
		view.Slots = append(view.Slots, SlotCap{
			Start: s.Start, CapacityQty: s.CapacityQty, RemainingQty: remain(s.CapacityQty, s.ReservedQty),
			CapacityOrders: s.CapacityOrders, RemainingOrders: remain(s.CapacityOrders, s.ReservedOrders),
		})
	}
	return view, nil
}

func (a *App) CapacitySettings(ctx context.Context, userID, brandID uuid.UUID) (CapacitySettings, error) {
	if _, _, err := a.OwnedBrand(ctx, userID, brandID); err != nil {
		return CapacitySettings{}, err
	}
	brand, err := loadBrandStored(ctx, a.Pool, brandID)
	if err != nil {
		return CapacitySettings{}, err
	}
	out := CapacitySettings{Brand: ruleInput(brand.capRule)}
	stores, err := a.Q.ListStores(ctx, brandID)
	if err != nil {
		return CapacitySettings{}, err
	}
	for _, st := range stores {
		over, err := loadStoreStored(ctx, a.Pool, st.ID)
		if err != nil {
			return CapacitySettings{}, err
		}
		eff := overlay(brand.capRule, over)
		row := StoreCapacitySetting{StoreID: st.ID, Name: st.Name, Inherit: !over.found, Effective: ruleInput(eff)}
		if over.found {
			in := ruleInput(over.capRule)
			row.Override = &in
		}
		out.Stores = append(out.Stores, row)
	}
	if out.Stores == nil {
		out.Stores = []StoreCapacitySetting{}
	}
	products, err := a.Q.ListProducts(ctx, brandID)
	if err != nil {
		return CapacitySettings{}, err
	}
	for _, p := range products {
		cap, lim, err := productRuleQuota(ctx, a.Pool, p.ID)
		if err != nil {
			return CapacitySettings{}, err
		}
		out.Products = append(out.Products, ProductCapacitySetting{
			ProductID: p.ID, Name: p.Name, MinQty: lim.MinQty, MaxPerCustomer: lim.MaxPerCustomer, CapacityQty: cap,
		})
	}
	if out.Products == nil {
		out.Products = []ProductCapacitySetting{}
	}
	return out, nil
}

func (a *App) SaveBrandCapacity(ctx context.Context, userID, brandID uuid.UUID, in CapacityInput) error {
	if _, _, err := a.OwnedBrand(ctx, userID, brandID); err != nil {
		return err
	}
	r, err := a.normalizeCapacity(in)
	if err != nil {
		return err
	}
	_, err = a.Pool.Exec(ctx, `
		INSERT INTO po_capacity_rules (
			brand_id, store_id, min_qty, min_amount_rp, max_qty_per_customer,
			capacity_qty, capacity_orders, min_proceed_qty, cutoff_lead_minutes, waitlist_enabled, updated_at
		) VALUES ($1, NULL, $2, $3, $4, $5, $6, $7, $8, $9, now())
		ON CONFLICT (brand_id) WHERE store_id IS NULL DO UPDATE SET
			min_qty = EXCLUDED.min_qty,
			min_amount_rp = EXCLUDED.min_amount_rp,
			max_qty_per_customer = EXCLUDED.max_qty_per_customer,
			capacity_qty = EXCLUDED.capacity_qty,
			capacity_orders = EXCLUDED.capacity_orders,
			min_proceed_qty = EXCLUDED.min_proceed_qty,
			cutoff_lead_minutes = EXCLUDED.cutoff_lead_minutes,
			waitlist_enabled = EXCLUDED.waitlist_enabled,
			updated_at = now()
	`, brandID, r.MinQty, r.MinAmountRp, r.MaxQtyPerCustomer, r.CapacityQty, r.CapacityOrders, r.MinProceedQty, r.CutoffLeadMinutes, r.WaitlistEnabled)
	return err
}

func (a *App) SaveStoreCapacity(ctx context.Context, userID, storeID uuid.UUID, in CapacityInput, inherit bool) error {
	st, err := a.Q.GetStore(ctx, storeID)
	if err != nil {
		return err
	}
	if _, _, err := a.OwnedBrand(ctx, userID, st.BrandID); err != nil {
		return err
	}
	if inherit {
		_, err = a.Pool.Exec(ctx, `DELETE FROM po_capacity_rules WHERE store_id = $1`, storeID)
		return err
	}
	r, err := a.normalizeCapacity(in)
	if err != nil {
		return err
	}
	_, err = a.Pool.Exec(ctx, `
		INSERT INTO po_capacity_rules (
			brand_id, store_id, min_qty, min_amount_rp, max_qty_per_customer,
			capacity_qty, capacity_orders, min_proceed_qty, cutoff_lead_minutes, waitlist_enabled, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, now())
		ON CONFLICT (store_id) WHERE store_id IS NOT NULL DO UPDATE SET
			min_qty = EXCLUDED.min_qty,
			min_amount_rp = EXCLUDED.min_amount_rp,
			max_qty_per_customer = EXCLUDED.max_qty_per_customer,
			capacity_qty = EXCLUDED.capacity_qty,
			capacity_orders = EXCLUDED.capacity_orders,
			min_proceed_qty = EXCLUDED.min_proceed_qty,
			cutoff_lead_minutes = EXCLUDED.cutoff_lead_minutes,
			waitlist_enabled = EXCLUDED.waitlist_enabled,
			updated_at = now()
	`, st.BrandID, storeID, r.MinQty, r.MinAmountRp, r.MaxQtyPerCustomer, r.CapacityQty, r.CapacityOrders, r.MinProceedQty, r.CutoffLeadMinutes, r.WaitlistEnabled)
	return err
}

func (a *App) SaveProductCapacity(ctx context.Context, userID, productID uuid.UUID, in ProductCapacityInput) error {
	p, err := a.Q.GetProduct(ctx, productID)
	if err != nil {
		return err
	}
	if _, _, err := a.OwnedBrand(ctx, userID, p.BrandID); err != nil {
		return err
	}
	r := capRule{MinQty: in.MinQty, MaxQtyPerCustomer: in.MaxPerCustomer, CapacityQty: in.CapacityQty}
	if err := validateRule(r); err != nil {
		return err
	}
	if in.MinQty == nil && in.MaxPerCustomer == nil && in.CapacityQty == nil {
		_, err = a.Pool.Exec(ctx, `DELETE FROM product_capacity_rules WHERE product_id = $1`, productID)
		return err
	}
	_, err = a.Pool.Exec(ctx, `
		INSERT INTO product_capacity_rules (product_id, min_qty, max_qty_per_customer, capacity_qty, updated_at)
		VALUES ($1, $2, $3, $4, now())
		ON CONFLICT (product_id) DO UPDATE SET
			min_qty = EXCLUDED.min_qty,
			max_qty_per_customer = EXCLUDED.max_qty_per_customer,
			capacity_qty = EXCLUDED.capacity_qty,
			updated_at = now()
	`, productID, in.MinQty, in.MaxPerCustomer, in.CapacityQty)
	return err
}

func (a *App) CapacityStats(ctx context.Context) ([]StoreCapacityStat, error) {
	rows, err := a.Pool.Query(ctx, `
		SELECT s.id, s.name, br.name,
			COALESCE(SUM(i.qty) FILTER (WHERE i.kind = 'oversell_blocked'), 0)::int,
			COALESCE(SUM(i.qty) FILTER (WHERE i.kind = 'min_quota_cancel'), 0)::int
		FROM stores s
		JOIN brands br ON br.id = s.brand_id
		LEFT JOIN capacity_incidents i ON i.store_id = s.id
		WHERE s.archived_at IS NULL
		GROUP BY s.id, s.name, br.name
		ORDER BY br.name, s.name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StoreCapacityStat
	byID := map[uuid.UUID]int{}
	for rows.Next() {
		var s StoreCapacityStat
		if err := rows.Scan(&s.StoreID, &s.StoreName, &s.BrandName, &s.OversellBlocked, &s.Cancellations); err != nil {
			return nil, err
		}
		byID[s.StoreID] = len(out)
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if out == nil {
		out = []StoreCapacityStat{}
	}
	add := func(query string) error {
		rs, err := a.Pool.Query(ctx, query)
		if err != nil {
			return err
		}
		defer rs.Close()
		for rs.Next() {
			var id uuid.UUID
			var n int32
			if err := rs.Scan(&id, &n); err != nil {
				return err
			}
			if i, ok := byID[id]; ok {
				out[i].OversoldNow += n
			}
		}
		return rs.Err()
	}
	if err := add(`
		SELECT b.store_id, COALESCE(SUM(GREATEST(bi.sold_count - bi.quota, 0)), 0)::int
		FROM batch_items bi
		JOIN po_batches b ON b.id = bi.batch_id
		WHERE bi.quota IS NOT NULL AND bi.sold_count > bi.quota
		GROUP BY b.store_id
	`); err != nil {
		return nil, err
	}
	if err := add(`
		SELECT b.store_id, COALESCE(SUM(GREATEST(c.reserved_orders - c.capacity_orders, 0)), 0)::int
		FROM batch_capacities c
		JOIN po_batches b ON b.id = c.batch_id
		WHERE c.capacity_orders IS NOT NULL AND c.reserved_orders > c.capacity_orders
		GROUP BY b.store_id
	`); err != nil {
		return nil, err
	}
	if err := add(`
		SELECT b.store_id, COALESCE(SUM(GREATEST(s.sold - b.quota_total, 0)), 0)::int
		FROM po_batches b
		JOIN (SELECT batch_id, SUM(sold_count)::int AS sold FROM batch_items GROUP BY batch_id) s ON s.batch_id = b.id
		WHERE b.quota_total IS NOT NULL AND s.sold > b.quota_total
		GROUP BY b.store_id
	`); err != nil {
		return nil, err
	}
	return out, nil
}

func (a *App) countsAsOversell(ctx context.Context, batchID uuid.UUID, code string) bool {
	if code == "quota" || code == "capacity" {
		return true
	}
	if code != "batch_closed" {
		return false
	}
	var reason string
	_ = a.Pool.QueryRow(ctx, `SELECT closed_reason FROM batch_capacities WHERE batch_id = $1`, batchID).Scan(&reason)
	return reason == "full"
}

func (a *App) recordIncident(ctx context.Context, brandID, storeID, batchID uuid.UUID, kind string, qty int32) {
	if qty <= 0 {
		qty = 1
	}
	if _, err := a.Pool.Exec(ctx, `
		INSERT INTO capacity_incidents (store_id, brand_id, batch_id, kind, qty)
		VALUES ($1, $2, $3, $4, $5)
	`, storeID, brandID, batchID, kind, qty); err != nil {
		slog.Error("capacity incident", "err", err, "kind", kind)
	}
}

func (a *App) OrderExtras(ctx context.Context, orderID uuid.UUID) ([]CustomerNotice, bool, error) {
	rows, err := a.Pool.Query(ctx, `
		SELECT kind, title, body FROM customer_notices WHERE order_id = $1 ORDER BY created_at
	`, orderID)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	var notes []CustomerNotice
	for rows.Next() {
		var n CustomerNotice
		if err := rows.Scan(&n.Kind, &n.Title, &n.Body); err != nil {
			return nil, false, err
		}
		notes = append(notes, n)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	if notes == nil {
		notes = []CustomerNotice{}
	}
	var refund bool
	err = a.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM order_refund_flags WHERE order_id = $1)`, orderID).Scan(&refund)
	return notes, refund, err
}
