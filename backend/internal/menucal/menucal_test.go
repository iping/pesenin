package menucal

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestCutoffZones(t *testing.T) {
	svc := Civil(2026, 11, 16)
	jkt, err := Cutoff(svc, "Asia/Jakarta", 1, "18:00")
	if err != nil {
		t.Fatal(err)
	}
	if !jkt.Equal(time.Date(2026, 11, 15, 11, 0, 0, 0, time.UTC)) {
		t.Fatalf("jakarta %s", jkt.UTC())
	}
	wit, err := Cutoff(svc, "Asia/Jayapura", 1, "18:00")
	if err != nil {
		t.Fatal(err)
	}
	if !wit.Equal(time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)) {
		t.Fatalf("jayapura %s", wit.UTC())
	}
	wita, err := Cutoff(svc, "Asia/Makassar", 1, "18:00:00")
	if err != nil {
		t.Fatal(err)
	}
	if !wita.Equal(time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)) {
		t.Fatalf("makassar %s", wita.UTC())
	}
	now := time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)
	if !OrderingOpen(now, jkt) {
		t.Fatal("17:00 WIB should still be open for a next-day 18:00 cutoff")
	}
	if OrderingOpen(now, wit) {
		t.Fatal("19:00 WIT should be past an 18:00 cutoff")
	}
	if OrderingOpen(now, wita) {
		t.Fatal("18:00 WITA exactly should be closed")
	}
	same, err := Cutoff(svc, "Asia/Jakarta", 0, "09:30")
	if err != nil {
		t.Fatal(err)
	}
	if !same.Equal(time.Date(2026, 11, 16, 2, 30, 0, 0, time.UTC)) {
		t.Fatalf("same day %s", same.UTC())
	}
	if _, err := Cutoff(svc, "Asia/Jakarta", 1, "25:00"); err == nil {
		t.Fatal("bad clock")
	}
}

func TestTodayAndMonday(t *testing.T) {
	// 2026-11-15 23:00 WIB is still Sunday locally, and already Monday in Jayapura (01:00 WIT).
	now := time.Date(2026, 11, 15, 16, 0, 0, 0, time.UTC)
	jkt := Today(now, "Asia/Jakarta")
	if DateString(jkt) != "2026-11-15" {
		t.Fatalf("jakarta today %s", DateString(jkt))
	}
	wit := Today(now, "Asia/Jayapura")
	if DateString(wit) != "2026-11-16" {
		t.Fatalf("jayapura today %s", DateString(wit))
	}
	mon := MondayOf(jkt)
	if DateString(mon) != "2026-11-09" || mon.Weekday() != time.Monday {
		t.Fatalf("monday %s %s", DateString(mon), mon.Weekday())
	}
	if DateString(MondayOf(Civil(2026, 11, 16))) != "2026-11-16" {
		t.Fatal("monday stays")
	}
}

func TestResolveRotationAndOverride(t *testing.T) {
	nasi := uuid.New()
	soup := uuid.New()
	monday := Civil(2026, 11, 16)
	tpl := []TplItem{{
		Weekday: int(time.Monday), MealSlot: "lunch", ProductID: nasi, SortOrder: 1,
	}}
	got := Resolve(monday, tpl, nil, nil)
	if got.Source != "rotation" || len(got.Items) != 1 || got.Items[0].ProductID != nasi {
		t.Fatalf("rotation %+v", got)
	}
	tue := Resolve(monday.AddDate(0, 0, 1), tpl, nil, nil)
	if tue.Source != "empty" || len(tue.Items) != 0 {
		t.Fatalf("tuesday %+v", tue)
	}
	over := Resolve(monday, tpl, &Day{Source: "override"}, []Item{{ProductID: soup, MealSlot: "dinner"}})
	if over.Source != "override" || len(over.Items) != 1 || over.Items[0].ProductID != soup {
		t.Fatalf("override %+v", over)
	}
	closed := Resolve(monday, tpl, &Day{Source: "copy", Closed: true, Notes: "libur"}, []Item{{ProductID: soup}})
	if !closed.Closed || len(closed.Items) != 0 || closed.Notes != "libur" {
		t.Fatalf("closed %+v", closed)
	}
	copied := Resolve(monday.AddDate(0, 0, 7), nil, &Day{Source: "copy"}, []Item{{ProductID: nasi, MealSlot: "lunch"}})
	if copied.Source != "copy" || copied.Items[0].ProductID != nasi {
		t.Fatalf("copy %+v", copied)
	}
}

func TestScheduleHook(t *testing.T) {
	id := uuid.New()
	SetScheduleHook(func(storeID uuid.UUID, date time.Time) bool {
		return storeID == id && DateString(date) == "2026-11-16"
	})
	t.Cleanup(func() { SetScheduleHook(nil) })
	if !ScheduledClosed(id, Civil(2026, 11, 16)) {
		t.Fatal("hook should close the date")
	}
	if ScheduledClosed(uuid.New(), Civil(2026, 11, 16)) {
		t.Fatal("other store")
	}
}
