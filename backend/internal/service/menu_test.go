package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"pesenin.product.biz.id/api/internal/db"
	"pesenin.product.biz.id/api/internal/menucal"
)

func TestMenuCutoffTimezone(t *testing.T) {
	app, user, batch := fixture(t)
	ctx := context.Background()
	st := storeOf(t, app, batch.BrandID)
	product := firstProduct(t, app, batch.BrandID)
	setTZ(t, app, st.ID, "Asia/Jakarta")
	enableMenu(t, app, user.ID, st.ID, 1, "18:00", nil)
	mustMenuDay(t, app, user.ID, st.ID, "2026-11-17", product, "lunch", nil)
	// 16 Nov 17:00 WIB is still before an H-1 18:00 cutoff for 17 Nov.
	// The same instant is 19:00 WIT, past the cutoff.
	app.Now = func() time.Time { return time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC) }
	o, err := app.PlaceMenuOrder(ctx, st.ID, menuOrder("2026-11-17", "lunch", product, "081211110001"))
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := app.LoadOrder(ctx, o.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.MenuDate == nil || menucal.DateString(*loaded.MenuDate) != "2026-11-17" || loaded.MenuSlot != "lunch" {
		t.Fatalf("menu ref %v %s", loaded.MenuDate, loaded.MenuSlot)
	}
	setTZ(t, app, st.ID, "Asia/Jayapura")
	_, err = app.PlaceMenuOrder(ctx, st.ID, menuOrder("2026-11-17", "lunch", product, "081211110002"))
	requireCode(t, err, "cutoff")

	setTZ(t, app, st.ID, "Asia/Jakarta")
	app.Now = func() time.Time { return time.Date(2026, 11, 16, 10, 30, 0, 0, time.UTC) }
	_, err = app.PlaceMenuOrder(ctx, st.ID, menuOrder("2026-11-17", "lunch", product, "081211110003"))
	if err != nil {
		t.Fatal(err)
	}
}

func TestMenuRotationCopyAndAnchor(t *testing.T) {
	app, user, batch := fixture(t)
	ctx := context.Background()
	st := storeOf(t, app, batch.BrandID)
	product := firstProduct(t, app, batch.BrandID)
	other, err := app.SaveProduct(ctx, user.ID, batch.BrandID, nil, ProductIn{Name: "Sup", Price: 20000})
	if err != nil {
		t.Fatal(err)
	}
	app.Now = func() time.Time { return time.Date(2026, 11, 1, 2, 0, 0, 0, time.UTC) }
	tpl, err := app.SaveMenuTemplate(ctx, user.ID, batch.BrandID, nil, TemplateIn{
		Name: "Mingguan sehat",
		Items: []TplItemIn{{
			Weekday: int(time.Monday), MealSlot: "lunch", ProductID: product, SortOrder: 1,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.SaveStoreMenuSettings(ctx, user.ID, st.ID, MenuSettingsIn{
		Enabled: true, TemplateID: &tpl.ID, CutoffDaysBefore: 1, CutoffTime: "18:00",
		AllowPickup: true, AllowDelivery: false,
	}); err != nil {
		t.Fatal(err)
	}
	week, err := app.MenuWeek(ctx, user.ID, st.ID, "2026-11-16")
	if err != nil {
		t.Fatal(err)
	}
	monday, tuesday := findDay(t, week, "2026-11-16"), findDay(t, week, "2026-11-17")
	if monday.Source != "rotation" || len(monday.Items) != 1 || monday.Items[0].ProductID != product || !monday.Orderable {
		t.Fatalf("monday %+v", monday)
	}
	if tuesday.Source != "empty" || len(tuesday.Items) != 0 || tuesday.Orderable {
		t.Fatalf("tuesday %+v", tuesday)
	}
	if _, err := app.SaveMenuDay(ctx, user.ID, st.ID, "2026-11-17", MenuDayIn{
		Items: []MenuItemIn{{ProductID: other.ID, MealSlot: "dinner"}},
	}); err != nil {
		t.Fatal(err)
	}
	copied, err := app.CopyMenuWeek(ctx, user.ID, st.ID, "2026-11-16")
	if err != nil {
		t.Fatal(err)
	}
	if len(copied.Copied) != 7 || len(copied.Skipped) != 0 {
		t.Fatalf("copy %+v", copied)
	}
	if _, err := app.SaveMenuTemplate(ctx, user.ID, batch.BrandID, &tpl.ID, TemplateIn{
		Name: "Mingguan sehat",
		Items: []TplItemIn{{
			Weekday: int(time.Wednesday), MealSlot: "breakfast", ProductID: other.ID,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	again, err := app.MenuWeek(ctx, user.ID, st.ID, "2026-11-16")
	if err != nil {
		t.Fatal(err)
	}
	monday = findDay(t, again, "2026-11-16")
	if len(monday.Items) != 0 {
		t.Fatalf("template edit should drop monday rotation, got %+v", monday)
	}
	wed := findDay(t, again, "2026-11-18")
	if wed.Source != "rotation" || len(wed.Items) != 1 || wed.Items[0].MealSlot != "breakfast" {
		t.Fatalf("wednesday %+v", wed)
	}
	next, err := app.MenuWeek(ctx, user.ID, st.ID, "2026-11-23")
	if err != nil {
		t.Fatal(err)
	}
	kept := findDay(t, next, "2026-11-23")
	if kept.Source != "copy" || len(kept.Items) != 1 || kept.Items[0].ProductID != product {
		t.Fatalf("copied monday %+v", kept)
	}
	keptTue := findDay(t, next, "2026-11-24")
	if keptTue.Source != "copy" || len(keptTue.Items) != 1 || keptTue.Items[0].ProductID != other.ID {
		t.Fatalf("copied tuesday %+v", keptTue)
	}

	// A copied day keeps its snapshot. A later rotation day is materialised on the first order.
	o, err := app.PlaceMenuOrder(ctx, st.ID, menuOrder("2026-11-24", "dinner", other.ID, "081211110009"))
	if err != nil {
		t.Fatal(err)
	}
	rot, err := app.PlaceMenuOrder(ctx, st.ID, menuOrder("2026-11-18", "breakfast", other.ID, "081211110011"))
	if err != nil {
		t.Fatal(err)
	}
	_ = rot
	var source string
	if err := app.Pool.QueryRow(ctx, `SELECT source FROM store_menu_days WHERE store_id = $1 AND menu_date = '2026-11-18'`, st.ID).Scan(&source); err != nil {
		t.Fatal(err)
	}
	if source != "rotation" {
		t.Fatalf("source %s", source)
	}
	rows, err := app.Q.ListBatchesByBrand(ctx, batch.BrandID)
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range rows {
		ch, err := app.Q.BatchChannel(ctx, b.ID)
		if err != nil {
			t.Fatal(err)
		}
		if ch == "menu" {
			t.Fatal("menu anchor listed as a PO")
		}
	}
	var code string
	if err := app.Pool.QueryRow(ctx, `SELECT code FROM po_batches WHERE channel = 'menu' AND store_id = $1`, st.ID).Scan(&code); err != nil {
		t.Fatal(err)
	}
	_, err = app.PlaceOrder(ctx, code, PlaceOrder{
		Name: "Sari", WhatsApp: "081211110010", Method: "pickup",
		Items: []CartItem{{BatchItemID: uuid.New(), Qty: 1}},
	})
	requireCode(t, err, "menu_batch")
	if _, err := app.DuplicateBatch(ctx, user.ID, o.BatchID); err == nil {
		t.Fatal("duplicating the menu anchor should fail")
	}
	prep, err := app.MenuPrep(ctx, user.ID, st.ID, "2026-11-24", true)
	if err != nil {
		t.Fatal(err)
	}
	if prep.Total != 1 || len(prep.Groups) != 1 || prep.Groups[0].Slot != "dinner" {
		t.Fatalf("prep %+v", prep)
	}
	runs, err := app.MenuRuns(ctx, user.ID, st.ID, "2026-11-24", true)
	if err != nil || len(runs.Rows) != 1 || runs.Rows[0].Summary == "" {
		t.Fatalf("runs %+v %v", runs, err)
	}
}

func TestMenuQuotaCapacityAndClosed(t *testing.T) {
	app, user, batch := fixture(t)
	ctx := context.Background()
	st := storeOf(t, app, batch.BrandID)
	product := firstProduct(t, app, batch.BrandID)
	app.Now = func() time.Time { return time.Date(2026, 11, 10, 2, 0, 0, 0, time.UTC) }
	one := int32(1)
	enableMenu(t, app, user.ID, st.ID, 1, "18:00", &one)
	mustMenuDay(t, app, user.ID, st.ID, "2026-11-16", product, "lunch", &one)
	first, err := app.PlaceMenuOrder(ctx, st.ID, menuOrder("2026-11-16", "lunch", product, "081211110021"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = app.PlaceMenuOrder(ctx, st.ID, menuOrder("2026-11-16", "lunch", product, "081211110022"))
	requireCode(t, err, "quota")
	if _, err := app.CancelOrder(ctx, user.ID, first.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := app.PlaceMenuOrder(ctx, st.ID, menuOrder("2026-11-16", "lunch", product, "081211110023")); err != nil {
		t.Fatal(err)
	}

	capTwo := int32(2)
	if _, err := app.SaveStoreMenuSettings(ctx, user.ID, st.ID, MenuSettingsIn{
		Enabled: true, CutoffDaysBefore: 0, CutoffTime: "21:00", AllowPickup: true, DayCapacity: &one,
	}); err != nil {
		t.Fatal(err)
	}
	// Same-day cutoff is 21:00 WIB = 14:00 UTC. Now is 02:00 UTC, still open.
	mustMenuDay(t, app, user.ID, st.ID, "2026-11-10", product, "", &capTwo)
	if _, err := app.PlaceMenuOrder(ctx, st.ID, menuOrder("2026-11-10", "", product, "081211110024")); err != nil {
		t.Fatal(err)
	}
	_, err = app.PlaceMenuOrder(ctx, st.ID, menuOrder("2026-11-10", "", product, "081211110025"))
	requireCode(t, err, "day_capacity")

	if _, err := app.SaveMenuDay(ctx, user.ID, st.ID, "2026-11-12", MenuDayIn{Closed: true}); err != nil {
		t.Fatal(err)
	}
	_, err = app.PlaceMenuOrder(ctx, st.ID, menuOrder("2026-11-12", "", product, "081211110026"))
	requireCode(t, err, "menu_closed")

	if _, err := app.Pool.Exec(ctx, `UPDATE stores SET is_open = false, accept_preorders_when_closed = false WHERE id = $1`, st.ID); err != nil {
		t.Fatal(err)
	}
	_, err = app.PlaceMenuOrder(ctx, st.ID, menuOrder("2026-11-11", "", product, "081211110027"))
	requireCode(t, err, "store_closed")

	menucal.SetScheduleHook(func(storeID uuid.UUID, date time.Time) bool {
		return storeID == st.ID && menucal.DateString(date) == "2026-11-13"
	})
	t.Cleanup(func() { menucal.SetScheduleHook(nil) })
	if _, err := app.Pool.Exec(ctx, `UPDATE stores SET is_open = true WHERE id = $1`, st.ID); err != nil {
		t.Fatal(err)
	}
	mustMenuDay(t, app, user.ID, st.ID, "2026-11-13", product, "breakfast", nil)
	_, err = app.PlaceMenuOrder(ctx, st.ID, menuOrder("2026-11-13", "breakfast", product, "081211110028"))
	requireCode(t, err, "schedule_closed")
}

func storeOf(t *testing.T, app *App, brandID uuid.UUID) db.Store {
	t.Helper()
	rows, err := app.Q.ListStores(context.Background(), brandID)
	if err != nil || len(rows) == 0 {
		t.Fatal(err)
	}
	return rows[0]
}

func setTZ(t *testing.T, app *App, storeID uuid.UUID, tz string) {
	t.Helper()
	if _, err := app.Pool.Exec(context.Background(), `UPDATE stores SET timezone = $2 WHERE id = $1`, storeID, tz); err != nil {
		t.Fatal(err)
	}
}

func enableMenu(t *testing.T, app *App, userID, storeID uuid.UUID, days int32, clock string, cap *int32) {
	t.Helper()
	if _, err := app.SaveStoreMenuSettings(context.Background(), userID, storeID, MenuSettingsIn{
		Enabled: true, CutoffDaysBefore: days, CutoffTime: clock, AllowPickup: true, AllowDelivery: true, DayCapacity: cap,
	}); err != nil {
		t.Fatal(err)
	}
}

func mustMenuDay(t *testing.T, app *App, userID, storeID uuid.UUID, date string, product uuid.UUID, slot string, quota *int32) {
	t.Helper()
	if _, err := app.SaveMenuDay(context.Background(), userID, storeID, date, MenuDayIn{
		Items: []MenuItemIn{{ProductID: product, MealSlot: slot, Quota: quota}},
	}); err != nil {
		t.Fatal(err)
	}
}

func menuOrder(date, slot string, product uuid.UUID, wa string) MenuOrderIn {
	return MenuOrderIn{
		Date: date, MealSlot: slot, Name: "Sari", WhatsApp: wa, Method: "pickup",
		Items: []MenuLine{{ProductID: product, Qty: 1}},
	}
}

func findDay(t *testing.T, week MenuWeekView, date string) MenuDayView {
	t.Helper()
	for _, d := range week.Days {
		if d.Date == date {
			return d
		}
	}
	t.Fatalf("missing %s", date)
	return MenuDayView{}
}

func requireCode(t *testing.T, err error, code string) {
	t.Helper()
	var se *Error
	if !errors.As(err, &se) || se.Code != code {
		t.Fatalf("want %s, got %v", code, err)
	}
}
