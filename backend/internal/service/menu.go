package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"pesenin.product.biz.id/api/internal/capacity"
	"pesenin.product.biz.id/api/internal/clock"
	"pesenin.product.biz.id/api/internal/db"
	"pesenin.product.biz.id/api/internal/menucal"
	"pesenin.product.biz.id/api/internal/text"
	"pesenin.product.biz.id/api/internal/uniqueamount"
)

var menuTagLabels = map[string]string{
	"vegan":        "Vegan",
	"vegetarian":   "Vegetarian",
	"low-carb":     "Rendah karbo",
	"high-protein": "Tinggi protein",
	"gluten-free":  "Bebas gluten",
	"dairy-free":   "Bebas susu",
	"spicy":        "Pedas",
	"halal":        "Halal",
}

type MenuSettingsIn struct {
	Enabled          bool       `json:"enabled"`
	TemplateID       *uuid.UUID `json:"templateId"`
	CutoffDaysBefore int32      `json:"cutoffDaysBefore"`
	CutoffTime       string     `json:"cutoffTime"`
	AllowPickup      bool       `json:"allowPickup"`
	AllowDelivery    bool       `json:"allowDelivery"`
	DayCapacity      *int32     `json:"dayCapacity"`
}

type MenuSettingsView struct {
	Enabled          bool       `json:"enabled"`
	TemplateID       *uuid.UUID `json:"templateId"`
	CutoffDaysBefore int32      `json:"cutoffDaysBefore"`
	CutoffTime       string     `json:"cutoffTime"`
	CutoffLabel      string     `json:"cutoffLabel"`
	AllowPickup      bool       `json:"allowPickup"`
	AllowDelivery    bool       `json:"allowDelivery"`
	DayCapacity      *int32     `json:"dayCapacity"`
	Timezone         string     `json:"timezone"`
}

type TplItemIn struct {
	Weekday   int        `json:"weekday"`
	MealSlot  string     `json:"mealSlot"`
	ProductID uuid.UUID  `json:"productId"`
	VariantID *uuid.UUID `json:"variantId"`
	SortOrder int32      `json:"sortOrder"`
	Quota     *int32     `json:"quota"`
}

type TemplateIn struct {
	Name  string      `json:"name"`
	Items []TplItemIn `json:"items"`
}

type TemplateItemView struct {
	Weekday   int        `json:"weekday"`
	MealSlot  string     `json:"mealSlot"`
	SlotLabel string     `json:"slotLabel"`
	ProductID uuid.UUID  `json:"productId"`
	VariantID *uuid.UUID `json:"variantId,omitempty"`
	SortOrder int32      `json:"sortOrder"`
	Quota     *int32     `json:"quota"`
}

type TemplateView struct {
	ID    uuid.UUID          `json:"id"`
	Name  string             `json:"name"`
	Items []TemplateItemView `json:"items"`
}

type MenuItemIn struct {
	ProductID uuid.UUID  `json:"productId"`
	VariantID *uuid.UUID `json:"variantId"`
	MealSlot  string     `json:"mealSlot"`
	SortOrder int32      `json:"sortOrder"`
	Quota     *int32     `json:"quota"`
}

type MenuDayIn struct {
	Closed bool         `json:"closed"`
	Notes  string       `json:"notes"`
	Items  []MenuItemIn `json:"items"`
}

type NutritionIn struct {
	Kcal      *int32   `json:"kcal"`
	ProteinG  *float64 `json:"proteinG"`
	CarbG     *float64 `json:"carbG"`
	FatG      *float64 `json:"fatG"`
	Allergens []string `json:"allergens"`
	Tags      []string `json:"tags"`
}

type NutritionView struct {
	Kcal      *int32   `json:"kcal"`
	ProteinG  *float64 `json:"proteinG"`
	CarbG     *float64 `json:"carbG"`
	FatG      *float64 `json:"fatG"`
	Allergens []string `json:"allergens"`
	Tags      []string `json:"tags"`
	TagLabels []string `json:"tagLabels"`
}

type MenuItemView struct {
	ID          *uuid.UUID     `json:"id,omitempty"`
	ProductID   uuid.UUID      `json:"productId"`
	VariantID   *uuid.UUID     `json:"variantId,omitempty"`
	Name        string         `json:"name"`
	VariantName string         `json:"variantName,omitempty"`
	Description string         `json:"description,omitempty"`
	PhotoURL    *string        `json:"photoUrl,omitempty"`
	Price       int64          `json:"price"`
	MealSlot    string         `json:"mealSlot"`
	SlotLabel   string         `json:"slotLabel"`
	Quota       *int32         `json:"quota"`
	Sold        int32          `json:"sold"`
	Remaining   any            `json:"remaining"`
	SortOrder   int32          `json:"sortOrder"`
	Active      bool           `json:"active"`
	Nutrition   *NutritionView `json:"nutrition,omitempty"`
}

type SlotView struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type MenuDayView struct {
	Date      string         `json:"date"`
	Label     string         `json:"label"`
	Weekday   int            `json:"weekday"`
	Source    string         `json:"source"`
	Closed    bool           `json:"closed"`
	Notes     string         `json:"notes"`
	Orderable bool           `json:"orderable"`
	Reason    string         `json:"reason,omitempty"`
	CutoffAt  string         `json:"cutoffAt"`
	Slots     []SlotView     `json:"slots"`
	Items     []MenuItemView `json:"items"`
}

type MenuWeekView struct {
	Enabled          bool          `json:"enabled"`
	Timezone         string        `json:"timezone"`
	CutoffDaysBefore int32         `json:"cutoffDaysBefore"`
	CutoffTime       string        `json:"cutoffTime"`
	CutoffLabel      string        `json:"cutoffLabel"`
	AllowPickup      bool          `json:"allowPickup"`
	AllowDelivery    bool          `json:"allowDelivery"`
	DayCapacity      *int32        `json:"dayCapacity"`
	TemplateID       *uuid.UUID    `json:"templateId"`
	Start            string        `json:"start"`
	ServerNow        string        `json:"serverNow"`
	Days             []MenuDayView `json:"days"`
}

type MenuLine struct {
	ProductID uuid.UUID  `json:"productId"`
	VariantID *uuid.UUID `json:"variantId"`
	Qty       int32      `json:"qty"`
}

type MenuOrderIn struct {
	Date           string     `json:"date"`
	MealSlot       string     `json:"mealSlot"`
	Name           string     `json:"name"`
	WhatsApp       string     `json:"whatsapp"`
	Method         string     `json:"method"`
	Address        string     `json:"address"`
	Note           string     `json:"note"`
	Items          []MenuLine `json:"items"`
	IdempotencyKey string     `json:"idempotencyKey"`
}

type MenuQuote struct {
	Subtotal int64
	Charges  []db.ExtraCharge
	Extra    int64
	Base     int64
}

type PrepItem struct {
	Name    string `json:"name"`
	Variant string `json:"variant,omitempty"`
	Qty     int32  `json:"qty"`
}

type PrepGroup struct {
	Slot  string     `json:"slot"`
	Label string     `json:"label"`
	Items []PrepItem `json:"items"`
}

type PrepList struct {
	Date   string      `json:"date"`
	Label  string      `json:"label"`
	Total  int32       `json:"total"`
	Groups []PrepGroup `json:"groups"`
}

type RunView struct {
	OrderID    uuid.UUID `json:"orderId"`
	Code       string    `json:"code"`
	Name       string    `json:"name"`
	WA         string    `json:"wa"`
	Method     string    `json:"method"`
	Address    string    `json:"address"`
	Note       string    `json:"note"`
	MealSlot   string    `json:"mealSlot"`
	SlotLabel  string    `json:"slotLabel"`
	Payment    string    `json:"payment"`
	Fulfilment string    `json:"fulfilment"`
	Summary    string    `json:"summary"`
}

type RunList struct {
	Date  string    `json:"date"`
	Label string    `json:"label"`
	Rows  []RunView `json:"rows"`
}

type CopySkip struct {
	Date   string `json:"date"`
	Reason string `json:"reason"`
}

type CopyResult struct {
	Copied  []string   `json:"copied"`
	Skipped []CopySkip `json:"skipped"`
}

func (a *App) ensureNotMenuAnchor(ctx context.Context, batchID uuid.UUID) error {
	ch, err := a.Q.BatchChannel(ctx, batchID)
	if err != nil {
		return err
	}
	if ch == "menu" {
		return E(400, "menu_batch", "Batch ini khusus menu harian.")
	}
	return nil
}

func (a *App) ownedStore(ctx context.Context, userID, storeID uuid.UUID) (db.Store, error) {
	st, err := a.Q.GetStore(ctx, storeID)
	if err != nil {
		return db.Store{}, err
	}
	if _, _, err := a.OwnedBrand(ctx, userID, st.BrandID); err != nil {
		return db.Store{}, err
	}
	return st, nil
}

func defaultMenuSettings(storeID uuid.UUID) db.MenuSettings {
	return db.MenuSettings{
		StoreID:          storeID,
		CutoffDaysBefore: 1,
		CutoffTime:       "18:00",
		AllowPickup:      true,
		AllowDelivery:    true,
	}
}

func settingsOrDefault(ctx context.Context, q *db.Queries, storeID uuid.UUID) (db.MenuSettings, error) {
	s, err := q.GetMenuSettings(ctx, storeID)
	if errors.Is(err, pgx.ErrNoRows) {
		return defaultMenuSettings(storeID), nil
	}
	return s, err
}

func settingsView(s db.MenuSettings, tz string) MenuSettingsView {
	return MenuSettingsView{
		Enabled: s.Enabled, TemplateID: s.TemplateID, CutoffDaysBefore: s.CutoffDaysBefore,
		CutoffTime: s.CutoffTime, CutoffLabel: cutoffLabel(s, tz),
		AllowPickup: s.AllowPickup, AllowDelivery: s.AllowDelivery, DayCapacity: s.DayCapacity,
		Timezone: tz,
	}
}

func cutoffLabel(s db.MenuSettings, tz string) string {
	zone := clock.Zone(tz)
	if s.CutoffDaysBefore <= 0 {
		return "Hari-H pukul " + s.CutoffTime + " " + zone
	}
	return fmt.Sprintf("H-%d pukul %s %s", s.CutoffDaysBefore, s.CutoffTime, zone)
}

func (a *App) GetStoreMenuSettings(ctx context.Context, userID, storeID uuid.UUID) (MenuSettingsView, error) {
	st, err := a.ownedStore(ctx, userID, storeID)
	if err != nil {
		return MenuSettingsView{}, err
	}
	s, err := settingsOrDefault(ctx, a.Q, storeID)
	if err != nil {
		return MenuSettingsView{}, err
	}
	return settingsView(s, st.Timezone), nil
}

func (a *App) SaveStoreMenuSettings(ctx context.Context, userID, storeID uuid.UUID, in MenuSettingsIn) (MenuSettingsView, error) {
	st, err := a.ownedStore(ctx, userID, storeID)
	if err != nil {
		return MenuSettingsView{}, err
	}
	if in.CutoffDaysBefore < 0 || in.CutoffDaysBefore > 14 {
		return MenuSettingsView{}, E(400, "bad_cutoff", "Batas pesan antara H-0 dan H-14.")
	}
	clockHM, err := menucal.NormalizeClock(in.CutoffTime)
	if err != nil {
		return MenuSettingsView{}, E(400, "bad_cutoff", "Jam tutup belum pas. Contoh 18:00.")
	}
	if !in.AllowPickup && !in.AllowDelivery {
		return MenuSettingsView{}, E(400, "bad_method", "Pilih ambil sendiri, diantar, atau dua-duanya.")
	}
	if in.DayCapacity != nil && *in.DayCapacity < 0 {
		return MenuSettingsView{}, E(400, "bad_quota", "Kuota harian tidak boleh minus.")
	}
	if in.TemplateID != nil {
		tpl, err := a.Q.GetMenuTemplate(ctx, *in.TemplateID)
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && tpl.BrandID != st.BrandID) {
			return MenuSettingsView{}, E(400, "bad_template", "Template itu bukan milik brand ini.")
		}
		if err != nil {
			return MenuSettingsView{}, err
		}
	}
	row := db.MenuSettings{
		StoreID: storeID, Enabled: in.Enabled, TemplateID: in.TemplateID,
		CutoffDaysBefore: in.CutoffDaysBefore, CutoffTime: clockHM,
		AllowPickup: in.AllowPickup, AllowDelivery: in.AllowDelivery, DayCapacity: in.DayCapacity,
	}
	if err := a.Q.UpsertMenuSettings(ctx, row); err != nil {
		return MenuSettingsView{}, err
	}
	return settingsView(row, st.Timezone), nil
}

func (a *App) ListMenuTemplates(ctx context.Context, userID, brandID uuid.UUID) ([]TemplateView, error) {
	if _, _, err := a.OwnedBrand(ctx, userID, brandID); err != nil {
		return nil, err
	}
	rows, err := a.Q.ListMenuTemplates(ctx, brandID)
	if err != nil {
		return nil, err
	}
	out := []TemplateView{}
	for _, t := range rows {
		view, err := a.templateView(ctx, t)
		if err != nil {
			return nil, err
		}
		out = append(out, view)
	}
	return out, nil
}

func (a *App) templateView(ctx context.Context, t db.MenuTemplate) (TemplateView, error) {
	items, err := a.Q.ListTemplateItems(ctx, t.ID)
	if err != nil {
		return TemplateView{}, err
	}
	view := TemplateView{ID: t.ID, Name: t.Name, Items: []TemplateItemView{}}
	for _, it := range items {
		view.Items = append(view.Items, TemplateItemView{
			Weekday: int(it.Weekday), MealSlot: it.MealSlot, SlotLabel: menucal.SlotLabel(it.MealSlot),
			ProductID: it.ProductID, VariantID: it.VariantID, SortOrder: it.SortOrder, Quota: it.Quota,
		})
	}
	return view, nil
}

func (a *App) SaveMenuTemplate(ctx context.Context, userID, brandID uuid.UUID, templateID *uuid.UUID, in TemplateIn) (TemplateView, error) {
	if _, _, err := a.OwnedBrand(ctx, userID, brandID); err != nil {
		return TemplateView{}, err
	}
	name := strings.TrimSpace(in.Name)
	if name == "" || len([]rune(name)) > 60 {
		return TemplateView{}, E(400, "bad_template", "Nama template wajib, maksimal 60 huruf.")
	}
	if len(in.Items) > 80 {
		return TemplateView{}, E(400, "bad_template", "Template maksimal 80 item.")
	}
	rows := make([]db.MenuTplItem, 0, len(in.Items))
	for i, it := range in.Items {
		if it.Weekday < 0 || it.Weekday > 6 || !menucal.ValidSlot(it.MealSlot) {
			return TemplateView{}, E(400, "bad_template", "Hari atau waktu makan belum pas.")
		}
		if it.Quota != nil && *it.Quota < 0 {
			return TemplateView{}, E(400, "bad_quota", "Kuota tidak boleh minus.")
		}
		if _, _, err := a.menuProduct(ctx, a.Q, brandID, it.ProductID, it.VariantID, false); err != nil {
			return TemplateView{}, err
		}
		sort := it.SortOrder
		if sort == 0 {
			sort = int32(i)
		}
		rows = append(rows, db.MenuTplItem{
			Weekday: int32(it.Weekday), MealSlot: it.MealSlot, ProductID: it.ProductID,
			VariantID: it.VariantID, SortOrder: sort, Quota: cloneQuota(it.Quota),
		})
	}
	var saved db.MenuTemplate
	err := a.tx(ctx, func(q *db.Queries) error {
		var err error
		if templateID == nil {
			saved, err = q.InsertMenuTemplate(ctx, brandID, name)
			if err != nil {
				return err
			}
		} else {
			cur, err := q.GetMenuTemplate(ctx, *templateID)
			if err != nil {
				return err
			}
			if cur.BrandID != brandID {
				return E(404, "not_found", "Template tidak ketemu.")
			}
			if err := q.RenameMenuTemplate(ctx, cur.ID, name); err != nil {
				return err
			}
			saved = cur
			saved.Name = name
		}
		return q.ReplaceTemplateItems(ctx, saved.ID, rows)
	})
	if err != nil {
		return TemplateView{}, err
	}
	return a.templateView(ctx, saved)
}

func (a *App) MenuWeek(ctx context.Context, userID, storeID uuid.UUID, start string) (MenuWeekView, error) {
	st, err := a.ownedStore(ctx, userID, storeID)
	if err != nil {
		return MenuWeekView{}, err
	}
	return a.week(ctx, st, start, 7, true)
}

func (a *App) PublicMenuBySlug(ctx context.Context, brandSlug, storeSlug, start string) (MenuWeekView, error) {
	brand, err := a.Q.GetBrandBySlug(ctx, brandSlug)
	if err != nil {
		return MenuWeekView{}, err
	}
	var st db.Store
	if storeSlug == "" {
		stores, err := a.Q.ListStores(ctx, brand.ID)
		if err != nil {
			return MenuWeekView{}, err
		}
		if len(stores) == 0 {
			return MenuWeekView{}, E(404, "not_found", "Toko belum ada.")
		}
		st = stores[0]
	} else {
		st, err = a.Q.GetStoreBySlug(ctx, db.GetStoreBySlugParams{BrandID: brand.ID, Slug: storeSlug})
		if err != nil {
			return MenuWeekView{}, err
		}
	}
	view, err := a.PublicMenu(ctx, st, start)
	if err != nil {
		return MenuWeekView{}, err
	}
	return view, nil
}

func (a *App) PublicMenu(ctx context.Context, st db.Store, start string) (MenuWeekView, error) {
	view, err := a.week(ctx, st, start, 7, false)
	if err != nil {
		return MenuWeekView{}, err
	}
	if !view.Enabled {
		view.Days = []MenuDayView{}
	}
	return view, nil
}

func (a *App) week(ctx context.Context, st db.Store, start string, n int, seller bool) (MenuWeekView, error) {
	settings, err := settingsOrDefault(ctx, a.Q, st.ID)
	if err != nil {
		return MenuWeekView{}, err
	}
	var from time.Time
	if strings.TrimSpace(start) == "" {
		today := menucal.Today(a.now(), st.Timezone)
		if seller {
			from = menucal.MondayOf(today)
		} else {
			from = today
		}
	} else {
		d, err := menucal.ParseDate(start)
		if err != nil {
			return MenuWeekView{}, E(400, "bad_date", "Tanggal belum pas.")
		}
		if seller {
			from = menucal.MondayOf(d)
		} else {
			from = d
		}
	}
	until := from.AddDate(0, 0, n)
	tpl, err := loadTpl(ctx, a.Q, settings.TemplateID)
	if err != nil {
		return MenuWeekView{}, err
	}
	persisted, err := a.Q.ListMenuDays(ctx, st.ID, menucal.DateString(from), menucal.DateString(until))
	if err != nil {
		return MenuWeekView{}, err
	}
	byDate := map[string]db.MenuDayRow{}
	for _, d := range persisted {
		byDate[d.MenuDate] = d
	}
	cat, err := a.menuCatalog(ctx, st.BrandID)
	if err != nil {
		return MenuWeekView{}, err
	}
	now := a.now()
	view := MenuWeekView{
		Enabled: settings.Enabled, Timezone: st.Timezone,
		CutoffDaysBefore: settings.CutoffDaysBefore, CutoffTime: settings.CutoffTime,
		CutoffLabel: cutoffLabel(settings, st.Timezone),
		AllowPickup: settings.AllowPickup, AllowDelivery: settings.AllowDelivery,
		DayCapacity: settings.DayCapacity, TemplateID: settings.TemplateID,
		Start: menucal.DateString(from), ServerNow: now.UTC().Format(time.RFC3339),
		Days: []MenuDayView{},
	}
	for i := 0; i < n; i++ {
		day := from.AddDate(0, 0, i)
		key := menucal.DateString(day)
		var row *db.MenuDayRow
		var stored []db.MenuItemRow
		if got, ok := byDate[key]; ok {
			row = &got
			stored, err = a.Q.ListMenuItems(ctx, got.ID)
			if err != nil {
				return MenuWeekView{}, err
			}
		}
		resolved := resolveStored(day, tpl, row, stored)
		dv := a.dayView(st, settings, day, resolved, stored, cat, now, seller)
		view.Days = append(view.Days, dv)
	}
	return view, nil
}

func (a *App) SaveMenuDay(ctx context.Context, userID, storeID uuid.UUID, date string, in MenuDayIn) (MenuDayView, error) {
	st, err := a.ownedStore(ctx, userID, storeID)
	if err != nil {
		return MenuDayView{}, err
	}
	d, err := menucal.ParseDate(date)
	if err != nil {
		return MenuDayView{}, E(400, "bad_date", "Tanggal belum pas.")
	}
	if len(in.Notes) > 300 {
		return MenuDayView{}, E(400, "bad_day", "Catatan hari maksimal 300 huruf.")
	}
	if len(in.Items) > 40 {
		return MenuDayView{}, E(400, "bad_day", "Maksimal 40 item sehari.")
	}
	if err := a.tx(ctx, func(q *db.Queries) error {
		return replaceMenuDay(ctx, q, st.BrandID, storeID, menucal.DateString(d), "override", in.Closed, strings.TrimSpace(in.Notes), in.Items)
	}); err != nil {
		return MenuDayView{}, err
	}
	week, err := a.week(ctx, st, menucal.DateString(d), 7, true)
	if err != nil {
		return MenuDayView{}, err
	}
	want := menucal.DateString(d)
	for _, day := range week.Days {
		if day.Date == want {
			return day, nil
		}
	}
	return MenuDayView{}, E(500, "internal", "Hari tidak ketemu setelah disimpan.")
}

func (a *App) ClearMenuDay(ctx context.Context, userID, storeID uuid.UUID, date string) error {
	st, err := a.ownedStore(ctx, userID, storeID)
	if err != nil {
		return err
	}
	_ = st
	d, err := menucal.ParseDate(date)
	if err != nil {
		return E(400, "bad_date", "Tanggal belum pas.")
	}
	return a.tx(ctx, func(q *db.Queries) error {
		row, err := q.GetMenuDayForUpdate(ctx, storeID, menucal.DateString(d))
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		items, err := q.ListMenuItemsForUpdate(ctx, row.ID)
		if err != nil {
			return err
		}
		for _, it := range items {
			if it.SoldCount > 0 {
				return E(409, "day_sold", "Tanggal ini sudah ada pesanan, jadi tidak bisa kembali ke template.")
			}
		}
		return q.DeleteMenuDay(ctx, row.ID)
	})
}

func (a *App) CopyMenuWeek(ctx context.Context, userID, storeID uuid.UUID, from string) (CopyResult, error) {
	st, err := a.ownedStore(ctx, userID, storeID)
	if err != nil {
		return CopyResult{}, err
	}
	start, err := menucal.ParseDate(from)
	if err != nil {
		return CopyResult{}, E(400, "bad_date", "Tanggal belum pas.")
	}
	start = menucal.MondayOf(start)
	week, err := a.week(ctx, st, menucal.DateString(start), 7, true)
	if err != nil {
		return CopyResult{}, err
	}
	result := CopyResult{Copied: []string{}, Skipped: []CopySkip{}}
	err = a.tx(ctx, func(q *db.Queries) error {
		for _, day := range week.Days {
			src, err := menucal.ParseDate(day.Date)
			if err != nil {
				return err
			}
			dst := menucal.DateString(src.AddDate(0, 0, 7))
			items := make([]MenuItemIn, 0, len(day.Items))
			for _, it := range day.Items {
				items = append(items, MenuItemIn{
					ProductID: it.ProductID, VariantID: it.VariantID, MealSlot: it.MealSlot,
					SortOrder: it.SortOrder, Quota: cloneQuota(it.Quota),
				})
			}
			if err := replaceMenuDay(ctx, q, st.BrandID, storeID, dst, "copy", day.Closed, day.Notes, items); err != nil {
				var se *Error
				if errors.As(err, &se) && se.Code == "day_sold" {
					result.Skipped = append(result.Skipped, CopySkip{Date: dst, Reason: se.Message})
					continue
				}
				return err
			}
			result.Copied = append(result.Copied, dst)
		}
		return nil
	})
	return result, err
}

func replaceMenuDay(ctx context.Context, q *db.Queries, brandID, storeID uuid.UUID, date, source string, closed bool, notes string, items []MenuItemIn) error {
	if source != "override" && source != "copy" && source != "rotation" {
		source = "override"
	}
	row, err := q.GetMenuDayForUpdate(ctx, storeID, date)
	if errors.Is(err, pgx.ErrNoRows) {
		row, err = q.InsertMenuDay(ctx, storeID, date, source, closed, notes)
		if isUnique(err, "store_menu_days_store_id_menu_date_key") {
			row, err = q.GetMenuDayForUpdate(ctx, storeID, date)
		}
	}
	if err != nil {
		return err
	}
	existing, err := q.ListMenuItemsForUpdate(ctx, row.ID)
	if err != nil {
		return err
	}
	if closed {
		items = nil
	}
	planned := map[string]MenuItemIn{}
	seen := map[string]bool{}
	for _, in := range items {
		if !menucal.ValidSlot(in.MealSlot) {
			return E(400, "bad_slot", "Waktu makan belum pas.")
		}
		if in.Quota != nil && *in.Quota < 0 {
			return E(400, "bad_quota", "Kuota tidak boleh minus.")
		}
		if _, _, err := menuProductQ(ctx, q, brandID, in.ProductID, in.VariantID, false); err != nil {
			return err
		}
		key := menuKey(in.ProductID, in.VariantID, in.MealSlot)
		if seen[key] {
			return E(400, "bad_day", "Item yang sama tidak boleh dobel di waktu makan yang sama.")
		}
		seen[key] = true
		planned[key] = in
	}
	for _, old := range existing {
		key := menuKey(old.ProductID, old.VariantID, old.MealSlot)
		in, keep := planned[key]
		if old.SoldCount > 0 && !keep {
			return E(409, "day_sold", "Ada pesanan pada item yang mau dihapus. Selesaikan atau biarkan item itu.")
		}
		if keep && in.Quota != nil && *in.Quota < old.SoldCount {
			return E(409, "bad_quota", "Kuota tidak boleh lebih kecil dari yang sudah dipesan.")
		}
	}
	if err := q.UpdateMenuDayMeta(ctx, row.ID, source, closed, notes); err != nil {
		return err
	}
	left := map[string]db.MenuItemRow{}
	for _, it := range existing {
		left[menuKey(it.ProductID, it.VariantID, it.MealSlot)] = it
	}
	for i, in := range items {
		key := menuKey(in.ProductID, in.VariantID, in.MealSlot)
		sort := in.SortOrder
		if sort == 0 {
			sort = int32(i)
		}
		if old, ok := left[key]; ok {
			if err := q.UpdateMenuItem(ctx, old.ID, sort, cloneQuota(in.Quota)); err != nil {
				return err
			}
			delete(left, key)
			continue
		}
		if _, err := q.InsertMenuItem(ctx, db.MenuItemRow{
			MenuDayID: row.ID, ProductID: in.ProductID, VariantID: in.VariantID,
			MealSlot: in.MealSlot, SortOrder: sort, Quota: cloneQuota(in.Quota),
		}); err != nil {
			return err
		}
	}
	for _, old := range left {
		if old.SoldCount > 0 {
			return E(409, "day_sold", "Ada pesanan pada item yang mau dihapus. Selesaikan atau biarkan item itu.")
		}
		if err := q.DeleteMenuItem(ctx, old.ID); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) SaveNutrition(ctx context.Context, userID, productID uuid.UUID, in NutritionIn) (NutritionView, error) {
	p, err := a.Q.GetProduct(ctx, productID)
	if err != nil {
		return NutritionView{}, err
	}
	if _, _, err := a.OwnedBrand(ctx, userID, p.BrandID); err != nil {
		return NutritionView{}, err
	}
	view, row, clear, err := normalizeNutrition(productID, in)
	if err != nil {
		return NutritionView{}, err
	}
	if clear {
		return view, a.Q.DeleteNutrition(ctx, productID)
	}
	return view, a.Q.UpsertNutrition(ctx, row)
}

func normalizeNutrition(productID uuid.UUID, in NutritionIn) (NutritionView, db.NutritionRow, bool, error) {
	if in.Kcal != nil && (*in.Kcal < 0 || *in.Kcal > 20000) {
		return NutritionView{}, db.NutritionRow{}, false, E(400, "bad_nutrition", "Kalori belum pas.")
	}
	for _, g := range []*float64{in.ProteinG, in.CarbG, in.FatG} {
		if g != nil && (*g < 0 || *g > 1000) {
			return NutritionView{}, db.NutritionRow{}, false, E(400, "bad_nutrition", "Angka protein, karbo, atau lemak belum pas.")
		}
	}
	allergens := cleanList(in.Allergens, 12, 40)
	tags := []string{}
	labels := []string{}
	seen := map[string]bool{}
	for _, raw := range in.Tags {
		tag := strings.ToLower(strings.TrimSpace(raw))
		if tag == "" || seen[tag] {
			continue
		}
		label, ok := menuTagLabels[tag]
		if !ok {
			return NutritionView{}, db.NutritionRow{}, false, E(400, "bad_nutrition", "Tag belum dikenal: "+tag+".")
		}
		seen[tag] = true
		tags = append(tags, tag)
		labels = append(labels, label)
	}
	if len(tags) > 8 {
		return NutritionView{}, db.NutritionRow{}, false, E(400, "bad_nutrition", "Maksimal 8 tag.")
	}
	view := NutritionView{Kcal: in.Kcal, ProteinG: in.ProteinG, CarbG: in.CarbG, FatG: in.FatG, Allergens: allergens, Tags: tags, TagLabels: labels}
	if view.Allergens == nil {
		view.Allergens = []string{}
	}
	if view.Tags == nil {
		view.Tags = []string{}
	}
	clear := in.Kcal == nil && in.ProteinG == nil && in.CarbG == nil && in.FatG == nil && len(allergens) == 0 && len(tags) == 0
	row := db.NutritionRow{
		ProductID: productID, Kcal: in.Kcal, ProteinG: in.ProteinG, CarbG: in.CarbG, FatG: in.FatG,
		Allergens: allergens, Tags: tags,
	}
	return view, row, clear, nil
}

func cleanList(in []string, maxN, maxLen int) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, raw := range in {
		s := strings.TrimSpace(raw)
		if s == "" || seen[strings.ToLower(s)] {
			continue
		}
		if len([]rune(s)) > maxLen {
			s = string([]rune(s)[:maxLen])
		}
		seen[strings.ToLower(s)] = true
		out = append(out, s)
		if len(out) >= maxN {
			break
		}
	}
	return out
}

func (a *App) MenuPrep(ctx context.Context, userID, storeID uuid.UUID, date string, includeUnpaid bool) (PrepList, error) {
	if _, err := a.ownedStore(ctx, userID, storeID); err != nil {
		return PrepList{}, err
	}
	d, err := menucal.ParseDate(date)
	if err != nil {
		return PrepList{}, E(400, "bad_date", "Tanggal belum pas.")
	}
	rows, err := a.Q.MenuPrep(ctx, storeID, menucal.DateString(d), includeUnpaid)
	if err != nil {
		return PrepList{}, err
	}
	order := []string{"breakfast", "lunch", "dinner", ""}
	groups := map[string]*PrepGroup{}
	var total int32
	for _, slot := range order {
		groups[slot] = &PrepGroup{Slot: slot, Label: menucal.SlotLabel(slot), Items: []PrepItem{}}
	}
	for _, r := range rows {
		g := groups[r.MealSlot]
		if g == nil {
			g = &PrepGroup{Slot: r.MealSlot, Label: menucal.SlotLabel(r.MealSlot), Items: []PrepItem{}}
			groups[r.MealSlot] = g
			order = append(order, r.MealSlot)
		}
		variant := ""
		if r.VariantName != nil {
			variant = *r.VariantName
		}
		g.Items = append(g.Items, PrepItem{Name: r.ProductName, Variant: variant, Qty: r.Qty})
		total += r.Qty
	}
	list := PrepList{Date: menucal.DateString(d), Label: menucal.DayLabel(d), Total: total, Groups: []PrepGroup{}}
	for _, slot := range order {
		g := groups[slot]
		if g != nil && len(g.Items) > 0 {
			list.Groups = append(list.Groups, *g)
		}
	}
	return list, nil
}

func (a *App) MenuRuns(ctx context.Context, userID, storeID uuid.UUID, date string, includeUnpaid bool) (RunList, error) {
	if _, err := a.ownedStore(ctx, userID, storeID); err != nil {
		return RunList{}, err
	}
	d, err := menucal.ParseDate(date)
	if err != nil {
		return RunList{}, E(400, "bad_date", "Tanggal belum pas.")
	}
	rows, err := a.Q.MenuRuns(ctx, storeID, menucal.DateString(d), includeUnpaid)
	if err != nil {
		return RunList{}, err
	}
	list := RunList{Date: menucal.DateString(d), Label: menucal.DayLabel(d), Rows: []RunView{}}
	for _, r := range rows {
		list.Rows = append(list.Rows, RunView{
			OrderID: r.OrderID, Code: r.Code, Name: r.Name, WA: r.WA, Method: r.Method,
			Address: r.Address, Note: r.Note, MealSlot: r.MealSlot, SlotLabel: menucal.SlotLabel(r.MealSlot),
			Payment: r.Payment, Fulfilment: r.Fulfilment, Summary: r.Summary,
		})
	}
	return list, nil
}

func (a *App) MenuPrepCSV(ctx context.Context, userID, storeID uuid.UUID, date string, includeUnpaid bool) (string, error) {
	list, err := a.MenuPrep(ctx, userID, storeID, date, includeUnpaid)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString("tanggal,waktu,item,varian,jumlah\n")
	for _, g := range list.Groups {
		for _, it := range g.Items {
			fmt.Fprintf(&b, "%s,%s,%s,%s,%d\n", list.Date, csv(g.Label), csv(it.Name), csv(it.Variant), it.Qty)
		}
	}
	return b.String(), nil
}

func (a *App) MenuRunsCSV(ctx context.Context, userID, storeID uuid.UUID, date string, includeUnpaid bool) (string, error) {
	list, err := a.MenuRuns(ctx, userID, storeID, date, includeUnpaid)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString("tanggal,waktu,kode,nama,wa,metode,alamat,item,bayar,ambil\n")
	for _, r := range list.Rows {
		fmt.Fprintf(&b, "%s,%s,%s,%s,%s,%s,%s,%s,%s,%s\n",
			list.Date, csv(r.SlotLabel), csv(r.Code), csv(r.Name), csv(r.WA), r.Method, csv(r.Address), csv(r.Summary), r.Payment, r.Fulfilment)
	}
	return b.String(), nil
}

func (a *App) QuoteMenu(ctx context.Context, storeID uuid.UUID, in MenuOrderIn) (MenuQuote, error) {
	priced, charges, _, _, err := a.priceMenu(ctx, storeID, in, false)
	if err != nil {
		return MenuQuote{}, err
	}
	var sub int64
	for _, ln := range priced {
		sub += ln.price * int64(ln.qty)
	}
	var extra int64
	for _, c := range charges {
		extra += c.AmountRp
	}
	return MenuQuote{Subtotal: sub, Charges: charges, Extra: extra, Base: sub + extra}, nil
}

type pricedMenu struct {
	menuItemID  uuid.UUID
	productID   uuid.UUID
	variantID   *uuid.UUID
	name        string
	variantName *string
	price       int64
	qty         int32
	sold        int32
	quota       *int32
}

func (a *App) priceMenu(ctx context.Context, storeID uuid.UUID, in MenuOrderIn, placing bool) ([]pricedMenu, []db.ExtraCharge, db.Store, db.MenuSettings, error) {
	if !menucal.ValidSlot(in.MealSlot) {
		return nil, nil, db.Store{}, db.MenuSettings{}, E(400, "bad_slot", "Pilih waktu makan yang tersedia.")
	}
	date, err := menucal.ParseDate(in.Date)
	if err != nil {
		return nil, nil, db.Store{}, db.MenuSettings{}, E(400, "bad_date", "Tanggal belum pas.")
	}
	st, err := a.Q.GetStore(ctx, storeID)
	if err != nil {
		return nil, nil, db.Store{}, db.MenuSettings{}, err
	}
	if st.ArchivedAt.Valid {
		return nil, nil, db.Store{}, db.MenuSettings{}, E(404, "not_found", "Toko tidak ketemu.")
	}
	settings, err := settingsOrDefault(ctx, a.Q, storeID)
	if err != nil {
		return nil, nil, db.Store{}, db.MenuSettings{}, err
	}
	if err := a.orderingOpen(st, settings, date); err != nil {
		return nil, nil, db.Store{}, db.MenuSettings{}, err
	}
	if in.Method != "pickup" && in.Method != "delivery" {
		return nil, nil, db.Store{}, db.MenuSettings{}, E(400, "bad_method", "Pilih ambil sendiri atau diantar.")
	}
	if in.Method == "pickup" && !settings.AllowPickup {
		return nil, nil, db.Store{}, db.MenuSettings{}, E(400, "bad_method", "Menu ini tidak bisa diambil sendiri.")
	}
	if in.Method == "delivery" && !settings.AllowDelivery {
		return nil, nil, db.Store{}, db.MenuSettings{}, E(400, "bad_method", "Menu ini tidak diantar.")
	}
	if in.Method == "delivery" && strings.TrimSpace(in.Address) == "" && placing {
		return nil, nil, db.Store{}, db.MenuSettings{}, E(400, "need_address", "Isi alamat antar dulu.")
	}
	tpl, err := loadTpl(ctx, a.Q, settings.TemplateID)
	if err != nil {
		return nil, nil, db.Store{}, db.MenuSettings{}, err
	}
	row, err := a.Q.GetMenuDay(ctx, storeID, menucal.DateString(date))
	var stored []db.MenuItemRow
	var dayPtr *db.MenuDayRow
	if err == nil {
		dayPtr = &row
		stored, err = a.Q.ListMenuItems(ctx, row.ID)
		if err != nil {
			return nil, nil, db.Store{}, db.MenuSettings{}, err
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, db.Store{}, db.MenuSettings{}, err
	}
	resolved := resolveStored(date, tpl, dayPtr, stored)
	if resolved.Closed {
		return nil, nil, db.Store{}, db.MenuSettings{}, E(409, "menu_closed", "Tanggal ini libur.")
	}
	lines, err := matchMenuLines(in.MealSlot, in.Items, resolved, stored)
	if err != nil {
		return nil, nil, db.Store{}, db.MenuSettings{}, err
	}
	cat, err := a.menuCatalog(ctx, st.BrandID)
	if err != nil {
		return nil, nil, db.Store{}, db.MenuSettings{}, err
	}
	priced := make([]pricedMenu, 0, len(lines))
	var daySold int32
	seenSold := map[uuid.UUID]bool{}
	for _, row := range stored {
		if !seenSold[row.ID] {
			daySold += row.SoldCount
			seenSold[row.ID] = true
		}
	}
	demandLines := make([]capacity.Line, 0, len(lines))
	for _, ln := range lines {
		p, ok := cat.products[ln.ProductID]
		if !ok || !p.Active {
			return nil, nil, db.Store{}, db.MenuSettings{}, E(400, "bad_item", "Ada menu yang sudah tidak dijual.")
		}
		var variant *db.ProductVariant
		var vname *string
		price := p.BasePrice
		if ln.VariantID != nil {
			v, ok := cat.variants[*ln.VariantID]
			if !ok || v.ProductID != p.ID || !v.Active {
				return nil, nil, db.Store{}, db.MenuSettings{}, E(400, "bad_item", "Varian menu tidak ketemu.")
			}
			variant = &v
			vname = &v.Name
			price += v.PriceDelta
		}
		if price < 0 {
			return nil, nil, db.Store{}, db.MenuSettings{}, E(400, "bad_item", "Harga menu belum pas.")
		}
		_ = variant
		name := p.Name
		priced = append(priced, pricedMenu{
			menuItemID: ln.ID, productID: p.ID, variantID: ln.VariantID, name: name, variantName: vname,
			price: price, qty: ln.Qty, sold: ln.SoldCount, quota: ln.Quota,
		})
		demandLines = append(demandLines, capacity.Line{Name: name, Qty: ln.Qty, Sold: ln.SoldCount, Quota: ln.Quota})
	}
	checker := capacity.Default
	if checker == nil {
		checker = capacity.MenuChecker{}
	}
	dec := checker.Allow(ctx, capacity.DayDemand{
		StoreID: storeID, Date: menucal.DateString(date), MealSlot: in.MealSlot,
		DaySold: daySold, DayLimit: settings.DayCapacity, Lines: demandLines,
	})
	if !dec.OK {
		return nil, nil, db.Store{}, db.MenuSettings{}, E(409, dec.Code, dec.Message)
	}
	charges, err := a.Q.ListActiveCharges(ctx, st.BrandID)
	if err != nil {
		return nil, nil, db.Store{}, db.MenuSettings{}, err
	}
	return priced, applicableCharges(charges, in.Method), st, settings, nil
}

func (a *App) orderingOpen(st db.Store, settings db.MenuSettings, date time.Time) error {
	if !settings.Enabled {
		return E(403, "menu_off", "Menu harian belum dibuka.")
	}
	if menucal.ScheduledClosed(st.ID, date) {
		return E(409, "schedule_closed", "Toko libur di tanggal ini.")
	}
	if !st.IsOpen && !st.AcceptPreordersWhenClosed {
		return E(409, "store_closed", "Toko sedang tutup dan belum terima pre-order.")
	}
	cutoff, err := menucal.Cutoff(date, st.Timezone, int(settings.CutoffDaysBefore), settings.CutoffTime)
	if err != nil {
		return E(400, "bad_cutoff", "Jam tutup menu belum pas.")
	}
	if !menucal.OrderingOpen(a.now(), cutoff) {
		return E(409, "cutoff", "Pemesanan untuk tanggal ini sudah ditutup.")
	}
	return nil
}

func (a *App) PlaceMenuOrder(ctx context.Context, storeID uuid.UUID, in MenuOrderIn) (db.Order, error) {
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
	if _, _, _, _, err := a.priceMenu(ctx, storeID, in, true); err != nil {
		return db.Order{}, err
	}
	date, _ := menucal.ParseDate(in.Date)
	pay, err := a.LoadSettings(ctx)
	if err != nil {
		return db.Order{}, err
	}
	var created db.Order
	err = a.txConn(ctx, func(q *db.Queries, tx pgx.Tx) error {
		st, err := q.GetStore(ctx, storeID)
		if err != nil {
			return err
		}
		settings, err := settingsOrDefault(ctx, q, storeID)
		if err != nil {
			return err
		}
		if err := a.orderingOpen(st, settings, date); err != nil {
			return err
		}
		if err := materializeMenuDay(ctx, q, st, settings, date); err != nil {
			return err
		}
		day, err := q.GetMenuDayForUpdate(ctx, storeID, menucal.DateString(date))
		if err != nil {
			return err
		}
		if day.Closed {
			return E(409, "menu_closed", "Tanggal ini libur.")
		}
		stored, err := q.ListMenuItemsForUpdate(ctx, day.ID)
		if err != nil {
			return err
		}
		matched, err := matchMenuLines(in.MealSlot, in.Items, menucal.Resolved{Items: rowsToItems(stored)}, stored)
		if err != nil {
			return err
		}
		var daySold int32
		for _, it := range stored {
			daySold += it.SoldCount
		}
		demand := make([]capacity.Line, 0, len(matched))
		priced := make([]pricedMenu, 0, len(matched))
		for _, ln := range matched {
			p, v, err := menuProductQ(ctx, q, st.BrandID, ln.ProductID, ln.VariantID, true)
			if err != nil {
				return err
			}
			price := p.BasePrice
			var vname *string
			if v != nil {
				vname = &v.Name
				price += v.PriceDelta
			}
			if price < 0 {
				return E(400, "bad_item", "Harga menu belum pas.")
			}
			priced = append(priced, pricedMenu{
				menuItemID: ln.ID, productID: p.ID, variantID: ln.VariantID, name: p.Name, variantName: vname,
				price: price, qty: ln.Qty, sold: ln.SoldCount, quota: ln.Quota,
			})
			demand = append(demand, capacity.Line{Name: p.Name, Qty: ln.Qty, Sold: ln.SoldCount, Quota: ln.Quota})
		}
		checker := capacity.Default
		if checker == nil {
			checker = capacity.MenuChecker{}
		}
		dec := checker.Allow(ctx, capacity.DayDemand{
			StoreID: storeID, Date: menucal.DateString(date), MealSlot: in.MealSlot,
			DaySold: daySold, DayLimit: settings.DayCapacity, Lines: demand,
		})
		if !dec.OK {
			return E(409, dec.Code, dec.Message)
		}
		for _, ln := range priced {
			if err := q.AddMenuSold(ctx, ln.menuItemID, ln.qty); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return E(409, "quota", ln.name+" habis.")
				}
				return err
			}
		}
		charges, err := q.ListActiveCharges(ctx, st.BrandID)
		if err != nil {
			return err
		}
		applied := applicableCharges(charges, in.Method)
		var sub, extra int64
		for _, ln := range priced {
			sub += ln.price * int64(ln.qty)
		}
		for _, c := range applied {
			extra += c.AmountRp
		}
		base := sub + extra
		banks, err := q.ListBanks(ctx, st.BrandID)
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
		cust, err := q.UpsertCustomer(ctx, db.UpsertCustomerParams{BrandID: st.BrandID, Name: strings.TrimSpace(in.Name), WaNumberE164: wa})
		if err != nil {
			return err
		}
		var key *string
		if in.IdempotencyKey != "" {
			key = &in.IdempotencyKey
		}
		closes := a.now().Add(72 * time.Hour)
		exp := paymentExpiry(a.now(), closes, nil, nil, pay.PaymentDeadlineHours)
		code, err := uniqueBatchCode(ctx, q)
		if err != nil {
			return err
		}
		anchor, err := q.EnsureMenuAnchor(ctx, st.ID, st.BrandID, code)
		if err != nil {
			return err
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
				BatchID: anchor, BrandID: st.BrandID, StoreID: st.ID, CustomerID: cust.ID,
				Code: codeStr, PublicToken: token, IdempotencyKey: key,
				Subtotal: sub, ExtraChargesTotal: extra, BaseTotal: base,
				UniqueSuffix: suffix, TotalDue: total, BankAccountID: bank.ID,
				FulfilmentMethod: in.Method, Address: strings.TrimSpace(in.Address), Note: strings.TrimSpace(in.Note),
				ExpiresAt: pgtPtr(exp),
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
		if err := q.InsertMenuOrderDay(ctx, order.ID, st.ID, menucal.DateString(date), in.MealSlot); err != nil {
			return err
		}
		for i, ln := range priced {
			bi, err := q.InsertBatchItem(ctx, db.InsertBatchItemParams{
				BatchID: anchor, ProductID: ln.productID, VariantID: pguPtr(ln.variantID),
				ProductName: ln.name, VariantName: ln.variantName, Price: ln.price, SortOrder: int32(i),
			})
			if err != nil {
				return err
			}
			oi, err := q.InsertOrderItem(ctx, db.InsertOrderItemParams{
				OrderID: order.ID, BatchItemID: bi.ID, ProductName: ln.name, VariantName: ln.variantName,
				UnitPrice: ln.price, Qty: ln.qty,
			})
			if err != nil {
				return err
			}
			if err := q.InsertMenuOrderLine(ctx, oi.ID, ln.menuItemID, ln.qty); err != nil {
				return err
			}
		}
		for i, c := range applied {
			if _, err := q.InsertOrderCharge(ctx, db.InsertOrderChargeParams{
				OrderID: order.ID, ExtraChargeID: pgu(c.ID), Name: c.Name, AmountRp: c.AmountRp, ComputedRp: c.AmountRp, SortOrder: int32(i),
			}); err != nil {
				return err
			}
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
	slot := menucal.SlotLabel(in.MealSlot)
	a.notifyBrandOwner(ctx, created.BrandID, "order_created", "Pesanan menu",
		"Ping! Pesanan menu "+menucal.DayLabel(date)+" · "+slot+" dari "+in.Name+".",
		"/app/pesanan/"+created.ID.String(), "order_created:"+created.ID.String(), true)
	return created, nil
}

type matchedLine struct {
	db.MenuItemRow
	Qty int32
}

func matchMenuLines(slot string, want []MenuLine, resolved menucal.Resolved, stored []db.MenuItemRow) ([]matchedLine, error) {
	if len(want) == 0 {
		return nil, E(400, "empty_cart", "Keranjang masih kosong.")
	}
	byKey := map[string]db.MenuItemRow{}
	for _, row := range stored {
		byKey[menuKey(row.ProductID, row.VariantID, row.MealSlot)] = row
	}
	type acc struct {
		line matchedLine
	}
	merged := map[string]*acc{}
	var order []string
	for _, w := range want {
		if w.Qty <= 0 || w.Qty > 99 {
			return nil, E(400, "bad_qty", "Jumlah pesanan belum pas.")
		}
		item, ok := findResolved(resolved.Items, w.ProductID, w.VariantID, slot)
		if !ok {
			return nil, E(400, "bad_item", "Ada item yang bukan menu tanggal ini.")
		}
		key := menuKey(item.ProductID, item.VariantID, item.MealSlot)
		row, ok := byKey[key]
		if !ok {
			row = db.MenuItemRow{
				ProductID: item.ProductID, VariantID: item.VariantID, MealSlot: item.MealSlot,
				SortOrder: int32(item.SortOrder), Quota: item.Quota,
			}
		}
		if cur, ok := merged[key]; ok {
			cur.line.Qty += w.Qty
			continue
		}
		merged[key] = &acc{line: matchedLine{MenuItemRow: row, Qty: w.Qty}}
		order = append(order, key)
	}
	out := make([]matchedLine, 0, len(order))
	for _, key := range order {
		out = append(out, merged[key].line)
	}
	return out, nil
}

func findResolved(items []menucal.Item, product uuid.UUID, variant *uuid.UUID, slot string) (menucal.Item, bool) {
	var fallback *menucal.Item
	for i := range items {
		it := items[i]
		if it.ProductID != product || !menucal.SameVariant(it.VariantID, variant) {
			continue
		}
		if it.MealSlot == slot {
			return it, true
		}
		if slot != "" && it.MealSlot == "" && fallback == nil {
			cp := it
			fallback = &cp
		}
	}
	if fallback != nil {
		return *fallback, true
	}
	return menucal.Item{}, false
}

func materializeMenuDay(ctx context.Context, q *db.Queries, st db.Store, settings db.MenuSettings, date time.Time) error {
	dateStr := menucal.DateString(date)
	if _, err := q.GetMenuDayForUpdate(ctx, st.ID, dateStr); err == nil {
		return nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	tpl, err := loadTpl(ctx, q, settings.TemplateID)
	if err != nil {
		return err
	}
	resolved := menucal.Resolve(date, tpl, nil, nil)
	if resolved.Closed || len(resolved.Items) == 0 {
		return E(409, "menu_closed", "Tanggal ini belum punya menu.")
	}
	row, err := q.InsertMenuDay(ctx, st.ID, dateStr, "rotation", false, "")
	if isUnique(err, "store_menu_days_store_id_menu_date_key") {
		return nil
	}
	if err != nil {
		return err
	}
	for i, it := range resolved.Items {
		if _, err := q.InsertMenuItem(ctx, db.MenuItemRow{
			MenuDayID: row.ID, ProductID: it.ProductID, VariantID: it.VariantID,
			MealSlot: it.MealSlot, SortOrder: int32(it.SortOrder), Quota: cloneQuota(it.Quota),
		}); err != nil {
			return err
		}
		_ = i
	}
	return nil
}

func resolveStored(date time.Time, tpl []menucal.TplItem, row *db.MenuDayRow, stored []db.MenuItemRow) menucal.Resolved {
	if row == nil {
		return menucal.Resolve(date, tpl, nil, nil)
	}
	return menucal.Resolve(date, tpl, &menucal.Day{Source: row.Source, Closed: row.Closed, Notes: row.Notes}, rowsToItems(stored))
}

func rowsToItems(rows []db.MenuItemRow) []menucal.Item {
	out := make([]menucal.Item, 0, len(rows))
	for _, r := range rows {
		out = append(out, menucal.Item{
			ProductID: r.ProductID, VariantID: r.VariantID, MealSlot: r.MealSlot,
			SortOrder: int(r.SortOrder), Quota: r.Quota,
		})
	}
	return out
}

func loadTpl(ctx context.Context, q *db.Queries, id *uuid.UUID) ([]menucal.TplItem, error) {
	if id == nil {
		return nil, nil
	}
	rows, err := q.ListTemplateItems(ctx, *id)
	if err != nil {
		return nil, err
	}
	out := make([]menucal.TplItem, 0, len(rows))
	for _, r := range rows {
		out = append(out, menucal.TplItem{
			Weekday: int(r.Weekday), MealSlot: r.MealSlot, ProductID: r.ProductID,
			VariantID: r.VariantID, SortOrder: int(r.SortOrder), Quota: r.Quota,
		})
	}
	return out, nil
}

type menuCat struct {
	products  map[uuid.UUID]db.Product
	variants  map[uuid.UUID]db.ProductVariant
	nutrition map[uuid.UUID]db.NutritionRow
}

func (a *App) menuCatalog(ctx context.Context, brandID uuid.UUID) (menuCat, error) {
	products, err := a.Q.ListProducts(ctx, brandID)
	if err != nil {
		return menuCat{}, err
	}
	variants, err := a.Q.ListVariantsByBrand(ctx, brandID)
	if err != nil {
		return menuCat{}, err
	}
	nuts, err := a.Q.ListNutritionByBrand(ctx, brandID)
	if err != nil {
		return menuCat{}, err
	}
	cat := menuCat{
		products: map[uuid.UUID]db.Product{}, variants: map[uuid.UUID]db.ProductVariant{},
		nutrition: map[uuid.UUID]db.NutritionRow{},
	}
	for _, p := range products {
		cat.products[p.ID] = p
	}
	for _, v := range variants {
		cat.variants[v.ID] = v
	}
	for _, n := range nuts {
		cat.nutrition[n.ProductID] = n
	}
	return cat, nil
}

func (a *App) dayView(st db.Store, settings db.MenuSettings, date time.Time, resolved menucal.Resolved, stored []db.MenuItemRow, cat menuCat, now time.Time, seller bool) MenuDayView {
	byKey := map[string]db.MenuItemRow{}
	for _, row := range stored {
		byKey[menuKey(row.ProductID, row.VariantID, row.MealSlot)] = row
	}
	items := []MenuItemView{}
	slotSeen := map[string]bool{}
	for _, it := range resolved.Items {
		p, ok := cat.products[it.ProductID]
		if !ok {
			continue
		}
		if !seller && !p.Active {
			continue
		}
		row := byKey[menuKey(it.ProductID, it.VariantID, it.MealSlot)]
		price := p.BasePrice
		vname := ""
		if it.VariantID != nil {
			if v, ok := cat.variants[*it.VariantID]; ok {
				price += v.PriceDelta
				vname = v.Name
			}
		}
		sold := row.SoldCount
		quota := it.Quota
		if row.ID != uuid.Nil && row.Quota != nil {
			quota = row.Quota
		}
		var id *uuid.UUID
		if row.ID != uuid.Nil {
			cp := row.ID
			id = &cp
		}
		view := MenuItemView{
			ID: id, ProductID: it.ProductID, VariantID: it.VariantID, Name: p.Name, VariantName: vname,
			Description: p.Description, PhotoURL: p.PhotoUrl, Price: price, MealSlot: it.MealSlot,
			SlotLabel: menucal.SlotLabel(it.MealSlot), Quota: quota, Sold: sold, Remaining: remainingOf(quota, sold),
			SortOrder: int32(it.SortOrder), Active: p.Active,
		}
		if n, ok := cat.nutrition[p.ID]; ok {
			nv := nutritionView(n)
			view.Nutrition = &nv
		}
		items = append(items, view)
		if it.MealSlot != "" {
			slotSeen[it.MealSlot] = true
		}
	}
	slots := []SlotView{}
	for _, s := range []string{"breakfast", "lunch", "dinner"} {
		if slotSeen[s] {
			slots = append(slots, SlotView{ID: s, Label: menucal.SlotLabel(s)})
		}
	}
	cutoff, _ := menucal.Cutoff(date, st.Timezone, int(settings.CutoffDaysBefore), settings.CutoffTime)
	orderable, reason := dayOrderable(st, settings, date, resolved, len(items) > 0, now)
	return MenuDayView{
		Date: menucal.DateString(date), Label: menucal.DayLabel(date), Weekday: int(date.Weekday()),
		Source: resolved.Source, Closed: resolved.Closed, Notes: resolved.Notes,
		Orderable: orderable, Reason: reason, CutoffAt: cutoff.UTC().Format(time.RFC3339),
		Slots: slots, Items: items,
	}
}

func dayOrderable(st db.Store, settings db.MenuSettings, date time.Time, resolved menucal.Resolved, hasItems bool, now time.Time) (bool, string) {
	if !settings.Enabled {
		return false, "Menu belum diaktifkan."
	}
	if resolved.Closed {
		return false, "Tanggal ini libur."
	}
	if menucal.ScheduledClosed(st.ID, date) {
		return false, "Toko libur di tanggal ini."
	}
	if !st.IsOpen && !st.AcceptPreordersWhenClosed {
		return false, "Toko sedang tutup dan belum terima pre-order."
	}
	cutoff, err := menucal.Cutoff(date, st.Timezone, int(settings.CutoffDaysBefore), settings.CutoffTime)
	if err != nil || !menucal.OrderingOpen(now, cutoff) {
		return false, "Pemesanan untuk tanggal ini sudah ditutup."
	}
	if !hasItems {
		return false, "Menu tanggal ini masih kosong."
	}
	return true, ""
}

func remainingOf(quota *int32, sold int32) any {
	if quota == nil {
		return nil
	}
	left := *quota - sold
	if left < 0 {
		left = 0
	}
	return left
}

func nutritionView(n db.NutritionRow) NutritionView {
	labels := make([]string, 0, len(n.Tags))
	for _, tag := range n.Tags {
		if label, ok := menuTagLabels[tag]; ok {
			labels = append(labels, label)
		} else {
			labels = append(labels, tag)
		}
	}
	allergens := n.Allergens
	if allergens == nil {
		allergens = []string{}
	}
	tags := n.Tags
	if tags == nil {
		tags = []string{}
	}
	return NutritionView{
		Kcal: n.Kcal, ProteinG: n.ProteinG, CarbG: n.CarbG, FatG: n.FatG,
		Allergens: allergens, Tags: tags, TagLabels: labels,
	}
}

func (a *App) menuProduct(ctx context.Context, q *db.Queries, brandID, productID uuid.UUID, variantID *uuid.UUID, requireActive bool) (db.Product, *db.ProductVariant, error) {
	return menuProductQ(ctx, q, brandID, productID, variantID, requireActive)
}

func menuProductQ(ctx context.Context, q *db.Queries, brandID, productID uuid.UUID, variantID *uuid.UUID, requireActive bool) (db.Product, *db.ProductVariant, error) {
	p, err := q.GetProduct(ctx, productID)
	if err != nil || p.BrandID != brandID {
		if err == nil || errors.Is(err, pgx.ErrNoRows) {
			return db.Product{}, nil, E(400, "bad_item", "Produk bukan milik toko ini.")
		}
		return db.Product{}, nil, err
	}
	if requireActive && !p.Active {
		return db.Product{}, nil, E(400, "bad_item", p.Name+" sedang tidak dijual.")
	}
	if variantID == nil {
		return p, nil, nil
	}
	vars, err := q.ListVariantsByProduct(ctx, productID)
	if err != nil {
		return db.Product{}, nil, err
	}
	for _, v := range vars {
		if v.ID == *variantID {
			if requireActive && !v.Active {
				return db.Product{}, nil, E(400, "bad_item", "Varian sedang tidak dijual.")
			}
			cp := v
			return p, &cp, nil
		}
	}
	return db.Product{}, nil, E(400, "bad_item", "Varian tidak ketemu.")
}

func menuKey(product uuid.UUID, variant *uuid.UUID, slot string) string {
	v := "-"
	if variant != nil {
		v = variant.String()
	}
	return product.String() + "|" + v + "|" + slot
}

func cloneQuota(q *int32) *int32 {
	if q == nil {
		return nil
	}
	v := *q
	return &v
}
