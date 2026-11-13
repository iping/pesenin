package db

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// Hand-written queries for the daily menu. sqlc models stay on the PO tables.

type MenuSettings struct {
	StoreID          uuid.UUID
	Enabled          bool
	TemplateID       *uuid.UUID
	CutoffDaysBefore int32
	CutoffTime       string
	AllowPickup      bool
	AllowDelivery    bool
	DayCapacity      *int32
}

type MenuTemplate struct {
	ID      uuid.UUID
	BrandID uuid.UUID
	Name    string
}

type MenuTplItem struct {
	ID         uuid.UUID
	TemplateID uuid.UUID
	Weekday    int32
	MealSlot   string
	ProductID  uuid.UUID
	VariantID  *uuid.UUID
	SortOrder  int32
	Quota      *int32
}

type MenuDayRow struct {
	ID       uuid.UUID
	StoreID  uuid.UUID
	MenuDate string
	Source   string
	Closed   bool
	Notes    string
}

type MenuItemRow struct {
	ID        uuid.UUID
	MenuDayID uuid.UUID
	ProductID uuid.UUID
	VariantID *uuid.UUID
	MealSlot  string
	SortOrder int32
	Quota     *int32
	SoldCount int32
}

type NutritionRow struct {
	ProductID uuid.UUID
	Kcal      *int32
	ProteinG  *float64
	CarbG     *float64
	FatG      *float64
	Allergens []string
	Tags      []string
}

type MenuOrderRef struct {
	ServiceDate string
	MealSlot    string
}

type PrepRow struct {
	MealSlot    string
	ProductName string
	VariantName *string
	Qty         int32
}

type RunRow struct {
	OrderID    uuid.UUID
	Code       string
	Name       string
	WA         string
	Method     string
	Address    string
	Note       string
	MealSlot   string
	Payment    string
	Fulfilment string
	Summary    string
}

func uuidPtrFrom(id pgtype.UUID) *uuid.UUID {
	if !id.Valid {
		return nil
	}
	u := uuid.UUID(id.Bytes)
	return &u
}

func (q *Queries) BatchChannel(ctx context.Context, id uuid.UUID) (string, error) {
	var channel string
	err := q.db.QueryRow(ctx, `SELECT channel FROM po_batches WHERE id = $1`, id).Scan(&channel)
	return channel, err
}

func (q *Queries) GetMenuSettings(ctx context.Context, storeID uuid.UUID) (MenuSettings, error) {
	var s MenuSettings
	var tpl pgtype.UUID
	err := q.db.QueryRow(ctx, `
		SELECT store_id, enabled, template_id, cutoff_days_before, to_char(cutoff_time, 'HH24:MI'),
		       allow_pickup, allow_delivery, day_capacity
		FROM store_menu_settings WHERE store_id = $1`, storeID).Scan(
		&s.StoreID, &s.Enabled, &tpl, &s.CutoffDaysBefore, &s.CutoffTime,
		&s.AllowPickup, &s.AllowDelivery, &s.DayCapacity,
	)
	s.TemplateID = uuidPtrFrom(tpl)
	return s, err
}

func (q *Queries) UpsertMenuSettings(ctx context.Context, s MenuSettings) error {
	var tpl any
	if s.TemplateID != nil {
		tpl = *s.TemplateID
	}
	_, err := q.db.Exec(ctx, `
		INSERT INTO store_menu_settings (
			store_id, enabled, template_id, cutoff_days_before, cutoff_time,
			allow_pickup, allow_delivery, day_capacity, updated_at
		) VALUES ($1, $2, $3, $4, $5::time, $6, $7, $8, now())
		ON CONFLICT (store_id) DO UPDATE SET
			enabled = EXCLUDED.enabled,
			template_id = EXCLUDED.template_id,
			cutoff_days_before = EXCLUDED.cutoff_days_before,
			cutoff_time = EXCLUDED.cutoff_time,
			allow_pickup = EXCLUDED.allow_pickup,
			allow_delivery = EXCLUDED.allow_delivery,
			day_capacity = EXCLUDED.day_capacity,
			updated_at = now()`,
		s.StoreID, s.Enabled, tpl, s.CutoffDaysBefore, s.CutoffTime,
		s.AllowPickup, s.AllowDelivery, s.DayCapacity,
	)
	return err
}

func (q *Queries) ListMenuTemplates(ctx context.Context, brandID uuid.UUID) ([]MenuTemplate, error) {
	rows, err := q.db.Query(ctx, `SELECT id, brand_id, name FROM menu_templates WHERE brand_id = $1 ORDER BY created_at`, brandID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MenuTemplate
	for rows.Next() {
		var t MenuTemplate
		if err := rows.Scan(&t.ID, &t.BrandID, &t.Name); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (q *Queries) GetMenuTemplate(ctx context.Context, id uuid.UUID) (MenuTemplate, error) {
	var t MenuTemplate
	err := q.db.QueryRow(ctx, `SELECT id, brand_id, name FROM menu_templates WHERE id = $1`, id).Scan(&t.ID, &t.BrandID, &t.Name)
	return t, err
}

func (q *Queries) InsertMenuTemplate(ctx context.Context, brandID uuid.UUID, name string) (MenuTemplate, error) {
	var t MenuTemplate
	err := q.db.QueryRow(ctx, `
		INSERT INTO menu_templates (brand_id, name) VALUES ($1, $2)
		RETURNING id, brand_id, name`, brandID, name).Scan(&t.ID, &t.BrandID, &t.Name)
	return t, err
}

func (q *Queries) RenameMenuTemplate(ctx context.Context, id uuid.UUID, name string) error {
	_, err := q.db.Exec(ctx, `UPDATE menu_templates SET name = $2, updated_at = now() WHERE id = $1`, id, name)
	return err
}

func (q *Queries) ListTemplateItems(ctx context.Context, templateID uuid.UUID) ([]MenuTplItem, error) {
	rows, err := q.db.Query(ctx, `
		SELECT id, template_id, weekday, meal_slot, product_id, variant_id, sort_order, quota
		FROM menu_template_items WHERE template_id = $1
		ORDER BY weekday, sort_order, meal_slot`, templateID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTplItems(rows)
}

func scanTplItems(rows pgx.Rows) ([]MenuTplItem, error) {
	var out []MenuTplItem
	for rows.Next() {
		var it MenuTplItem
		var variant pgtype.UUID
		if err := rows.Scan(&it.ID, &it.TemplateID, &it.Weekday, &it.MealSlot, &it.ProductID, &variant, &it.SortOrder, &it.Quota); err != nil {
			return nil, err
		}
		it.VariantID = uuidPtrFrom(variant)
		out = append(out, it)
	}
	return out, rows.Err()
}

func (q *Queries) ReplaceTemplateItems(ctx context.Context, templateID uuid.UUID, items []MenuTplItem) error {
	if _, err := q.db.Exec(ctx, `DELETE FROM menu_template_items WHERE template_id = $1`, templateID); err != nil {
		return err
	}
	for _, it := range items {
		var variant any
		if it.VariantID != nil {
			variant = *it.VariantID
		}
		if _, err := q.db.Exec(ctx, `
			INSERT INTO menu_template_items (template_id, weekday, meal_slot, product_id, variant_id, sort_order, quota)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			templateID, it.Weekday, it.MealSlot, it.ProductID, variant, it.SortOrder, it.Quota); err != nil {
			return err
		}
	}
	return nil
}

func (q *Queries) ListMenuDays(ctx context.Context, storeID uuid.UUID, from, to string) ([]MenuDayRow, error) {
	rows, err := q.db.Query(ctx, `
		SELECT id, store_id, to_char(menu_date, 'YYYY-MM-DD'), source, closed, notes
		FROM store_menu_days
		WHERE store_id = $1 AND menu_date >= $2::date AND menu_date < $3::date
		ORDER BY menu_date`, storeID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MenuDayRow
	for rows.Next() {
		var d MenuDayRow
		if err := rows.Scan(&d.ID, &d.StoreID, &d.MenuDate, &d.Source, &d.Closed, &d.Notes); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (q *Queries) GetMenuDay(ctx context.Context, storeID uuid.UUID, date string) (MenuDayRow, error) {
	var d MenuDayRow
	err := q.db.QueryRow(ctx, `
		SELECT id, store_id, to_char(menu_date, 'YYYY-MM-DD'), source, closed, notes
		FROM store_menu_days WHERE store_id = $1 AND menu_date = $2::date`, storeID, date).Scan(
		&d.ID, &d.StoreID, &d.MenuDate, &d.Source, &d.Closed, &d.Notes,
	)
	return d, err
}

func (q *Queries) GetMenuDayForUpdate(ctx context.Context, storeID uuid.UUID, date string) (MenuDayRow, error) {
	var d MenuDayRow
	err := q.db.QueryRow(ctx, `
		SELECT id, store_id, to_char(menu_date, 'YYYY-MM-DD'), source, closed, notes
		FROM store_menu_days WHERE store_id = $1 AND menu_date = $2::date
		FOR UPDATE`, storeID, date).Scan(
		&d.ID, &d.StoreID, &d.MenuDate, &d.Source, &d.Closed, &d.Notes,
	)
	return d, err
}

func (q *Queries) InsertMenuDay(ctx context.Context, storeID uuid.UUID, date, source string, closed bool, notes string) (MenuDayRow, error) {
	var d MenuDayRow
	err := q.db.QueryRow(ctx, `
		INSERT INTO store_menu_days (store_id, menu_date, source, closed, notes)
		VALUES ($1, $2::date, $3, $4, $5)
		RETURNING id, store_id, to_char(menu_date, 'YYYY-MM-DD'), source, closed, notes`,
		storeID, date, source, closed, notes).Scan(&d.ID, &d.StoreID, &d.MenuDate, &d.Source, &d.Closed, &d.Notes)
	return d, err
}

func (q *Queries) UpdateMenuDayMeta(ctx context.Context, id uuid.UUID, source string, closed bool, notes string) error {
	_, err := q.db.Exec(ctx, `UPDATE store_menu_days SET source = $2, closed = $3, notes = $4 WHERE id = $1`, id, source, closed, notes)
	return err
}

func (q *Queries) DeleteMenuDay(ctx context.Context, id uuid.UUID) error {
	_, err := q.db.Exec(ctx, `DELETE FROM store_menu_days WHERE id = $1`, id)
	return err
}

func (q *Queries) ListMenuItems(ctx context.Context, dayID uuid.UUID) ([]MenuItemRow, error) {
	rows, err := q.db.Query(ctx, `
		SELECT id, menu_day_id, product_id, variant_id, meal_slot, sort_order, quota, sold_count
		FROM store_menu_items WHERE menu_day_id = $1
		ORDER BY sort_order, meal_slot`, dayID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMenuItems(rows)
}

func (q *Queries) ListMenuItemsForUpdate(ctx context.Context, dayID uuid.UUID) ([]MenuItemRow, error) {
	rows, err := q.db.Query(ctx, `
		SELECT id, menu_day_id, product_id, variant_id, meal_slot, sort_order, quota, sold_count
		FROM store_menu_items WHERE menu_day_id = $1
		ORDER BY id FOR UPDATE`, dayID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMenuItems(rows)
}

func scanMenuItems(rows pgx.Rows) ([]MenuItemRow, error) {
	var out []MenuItemRow
	for rows.Next() {
		var it MenuItemRow
		var variant pgtype.UUID
		if err := rows.Scan(&it.ID, &it.MenuDayID, &it.ProductID, &variant, &it.MealSlot, &it.SortOrder, &it.Quota, &it.SoldCount); err != nil {
			return nil, err
		}
		it.VariantID = uuidPtrFrom(variant)
		out = append(out, it)
	}
	return out, rows.Err()
}

func (q *Queries) InsertMenuItem(ctx context.Context, it MenuItemRow) (uuid.UUID, error) {
	var variant any
	if it.VariantID != nil {
		variant = *it.VariantID
	}
	var id uuid.UUID
	err := q.db.QueryRow(ctx, `
		INSERT INTO store_menu_items (menu_day_id, product_id, variant_id, meal_slot, sort_order, quota, sold_count)
		VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`,
		it.MenuDayID, it.ProductID, variant, it.MealSlot, it.SortOrder, it.Quota, it.SoldCount).Scan(&id)
	return id, err
}

func (q *Queries) UpdateMenuItem(ctx context.Context, id uuid.UUID, sort int32, quota *int32) error {
	_, err := q.db.Exec(ctx, `UPDATE store_menu_items SET sort_order = $2, quota = $3 WHERE id = $1`, id, sort, quota)
	return err
}

func (q *Queries) DeleteMenuItem(ctx context.Context, id uuid.UUID) error {
	_, err := q.db.Exec(ctx, `DELETE FROM store_menu_items WHERE id = $1`, id)
	return err
}

func (q *Queries) AddMenuSold(ctx context.Context, id uuid.UUID, qty int32) error {
	tag, err := q.db.Exec(ctx, `
		UPDATE store_menu_items
		SET sold_count = sold_count + $2
		WHERE id = $1 AND (quota IS NULL OR sold_count + $2 <= quota)`, id, qty)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func (q *Queries) SumMenuSold(ctx context.Context, dayID uuid.UUID) (int32, error) {
	var n int32
	err := q.db.QueryRow(ctx, `SELECT COALESCE(SUM(sold_count), 0)::int FROM store_menu_items WHERE menu_day_id = $1`, dayID).Scan(&n)
	return n, err
}

func (q *Queries) ReleaseMenuSold(ctx context.Context, orderID uuid.UUID) error {
	_, err := q.db.Exec(ctx, `
		UPDATE store_menu_items AS smi
		SET sold_count = GREATEST(0, smi.sold_count - mol.qty)
		FROM menu_order_lines mol
		JOIN order_items oi ON oi.id = mol.order_item_id
		WHERE mol.menu_item_id = smi.id AND oi.order_id = $1`, orderID)
	return err
}

func (q *Queries) InsertMenuOrderDay(ctx context.Context, orderID, storeID uuid.UUID, date, slot string) error {
	_, err := q.db.Exec(ctx, `
		INSERT INTO menu_order_days (order_id, store_id, service_date, meal_slot)
		VALUES ($1, $2, $3::date, $4)`, orderID, storeID, date, slot)
	return err
}

func (q *Queries) InsertMenuOrderLine(ctx context.Context, orderItemID, menuItemID uuid.UUID, qty int32) error {
	_, err := q.db.Exec(ctx, `
		INSERT INTO menu_order_lines (order_item_id, menu_item_id, qty) VALUES ($1, $2, $3)`,
		orderItemID, menuItemID, qty)
	return err
}

func (q *Queries) GetMenuOrderRef(ctx context.Context, orderID uuid.UUID) (MenuOrderRef, error) {
	var ref MenuOrderRef
	err := q.db.QueryRow(ctx, `
		SELECT to_char(service_date, 'YYYY-MM-DD'), meal_slot
		FROM menu_order_days WHERE order_id = $1`, orderID).Scan(&ref.ServiceDate, &ref.MealSlot)
	return ref, err
}

func (q *Queries) EnsureMenuAnchor(ctx context.Context, storeID, brandID uuid.UUID, code string) (uuid.UUID, error) {
	var id uuid.UUID
	err := q.db.QueryRow(ctx, `
		INSERT INTO po_batches (
			store_id, brand_id, code, title, status, opens_at, closes_at, ready_at,
			allow_pickup, allow_delivery, notes, channel
		) VALUES (
			$1, $2, $3, 'Menu harian', 'open', now(), '2099-01-01', now(),
			true, true, '', 'menu'
		)
		ON CONFLICT (store_id) WHERE channel = 'menu' DO NOTHING
		RETURNING id`, storeID, brandID, code).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, err
	}
	err = q.db.QueryRow(ctx, `SELECT id FROM po_batches WHERE store_id = $1 AND channel = 'menu'`, storeID).Scan(&id)
	return id, err
}

func (q *Queries) ListNutritionByBrand(ctx context.Context, brandID uuid.UUID) ([]NutritionRow, error) {
	rows, err := q.db.Query(ctx, `
		SELECT n.product_id, n.kcal, n.protein_g, n.carb_g, n.fat_g, n.allergens, n.tags
		FROM product_nutrition n
		JOIN products p ON p.id = n.product_id
		WHERE p.brand_id = $1`, brandID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []NutritionRow
	for rows.Next() {
		n, err := scanNutrition(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (q *Queries) ListNutritionByProducts(ctx context.Context, ids []uuid.UUID) ([]NutritionRow, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := q.db.Query(ctx, `
		SELECT product_id, kcal, protein_g, carb_g, fat_g, allergens, tags
		FROM product_nutrition WHERE product_id = ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []NutritionRow
	for rows.Next() {
		n, err := scanNutrition(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

type nutritionScanner interface {
	Scan(dest ...any) error
}

func scanNutrition(row nutritionScanner) (NutritionRow, error) {
	var n NutritionRow
	err := row.Scan(&n.ProductID, &n.Kcal, &n.ProteinG, &n.CarbG, &n.FatG, &n.Allergens, &n.Tags)
	if n.Allergens == nil {
		n.Allergens = []string{}
	}
	if n.Tags == nil {
		n.Tags = []string{}
	}
	return n, err
}

func (q *Queries) UpsertNutrition(ctx context.Context, n NutritionRow) error {
	if n.Allergens == nil {
		n.Allergens = []string{}
	}
	if n.Tags == nil {
		n.Tags = []string{}
	}
	_, err := q.db.Exec(ctx, `
		INSERT INTO product_nutrition (product_id, kcal, protein_g, carb_g, fat_g, allergens, tags)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (product_id) DO UPDATE SET
			kcal = EXCLUDED.kcal, protein_g = EXCLUDED.protein_g, carb_g = EXCLUDED.carb_g,
			fat_g = EXCLUDED.fat_g, allergens = EXCLUDED.allergens, tags = EXCLUDED.tags`,
		n.ProductID, n.Kcal, n.ProteinG, n.CarbG, n.FatG, n.Allergens, n.Tags)
	return err
}

func (q *Queries) DeleteNutrition(ctx context.Context, productID uuid.UUID) error {
	_, err := q.db.Exec(ctx, `DELETE FROM product_nutrition WHERE product_id = $1`, productID)
	return err
}

func (q *Queries) MenuPrep(ctx context.Context, storeID uuid.UUID, date string, includeUnpaid bool) ([]PrepRow, error) {
	rows, err := q.db.Query(ctx, `
		SELECT mod.meal_slot, oi.product_name, oi.variant_name, SUM(oi.qty)::int
		FROM menu_order_days mod
		JOIN orders o ON o.id = mod.order_id
		JOIN order_items oi ON oi.order_id = o.id
		WHERE mod.store_id = $1 AND mod.service_date = $2::date
		  AND (
		    o.payment_status = 'paid'
		    OR ($3 AND o.payment_status IN ('pending', 'proof_submitted'))
		  )
		GROUP BY mod.meal_slot, oi.product_name, oi.variant_name
		ORDER BY mod.meal_slot, oi.product_name`, storeID, date, includeUnpaid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PrepRow
	for rows.Next() {
		var r PrepRow
		if err := rows.Scan(&r.MealSlot, &r.ProductName, &r.VariantName, &r.Qty); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (q *Queries) MenuRuns(ctx context.Context, storeID uuid.UUID, date string, includeUnpaid bool) ([]RunRow, error) {
	rows, err := q.db.Query(ctx, `
		SELECT o.id, o.code, c.name, c.wa_number_e164, o.fulfilment_method, o.address, o.note,
		       mod.meal_slot, o.payment_status, o.fulfilment_status,
		       COALESCE(string_agg(oi.qty::text || '× ' || oi.product_name || COALESCE(' ' || oi.variant_name, ''), ', ' ORDER BY oi.product_name), '')
		FROM menu_order_days mod
		JOIN orders o ON o.id = mod.order_id
		JOIN customers c ON c.id = o.customer_id
		LEFT JOIN order_items oi ON oi.order_id = o.id
		WHERE mod.store_id = $1 AND mod.service_date = $2::date
		  AND (
		    o.payment_status = 'paid'
		    OR ($3 AND o.payment_status IN ('pending', 'proof_submitted'))
		  )
		GROUP BY o.id, o.code, c.name, c.wa_number_e164, o.fulfilment_method, o.address, o.note,
		         mod.meal_slot, o.payment_status, o.fulfilment_status, o.created_at
		ORDER BY mod.meal_slot, o.created_at`, storeID, date, includeUnpaid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RunRow
	for rows.Next() {
		var r RunRow
		if err := rows.Scan(&r.OrderID, &r.Code, &r.Name, &r.WA, &r.Method, &r.Address, &r.Note, &r.MealSlot, &r.Payment, &r.Fulfilment, &r.Summary); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
