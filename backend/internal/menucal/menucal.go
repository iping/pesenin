// Package menucal is the pure calendar for daily menus:
// cutoff instants in the store timezone, weekly rotation, and date overrides.
package menucal

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"pesenin.product.biz.id/api/internal/clock"
)

var dayShort = []string{"Min", "Sen", "Sel", "Rab", "Kam", "Jum", "Sab"}
var monthShort = []string{"Jan", "Feb", "Mar", "Apr", "Mei", "Jun", "Jul", "Agu", "Sep", "Okt", "Nov", "Des"}

// Slots customers can pick. Empty string means the item is available all day.
var Slots = []string{"", "breakfast", "lunch", "dinner"}

func ValidSlot(s string) bool {
	for _, v := range Slots {
		if s == v {
			return true
		}
	}
	return false
}

func SlotLabel(s string) string {
	switch s {
	case "breakfast":
		return "Sarapan"
	case "lunch":
		return "Makan siang"
	case "dinner":
		return "Makan malam"
	default:
		return "Sepanjang hari"
	}
}

// Civil is a calendar date with no clock, stored as UTC midnight.
func Civil(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

func ParseDate(s string) (time.Time, error) {
	t, err := time.Parse("2006-01-02", strings.TrimSpace(s))
	if err != nil {
		return time.Time{}, fmt.Errorf("bad date")
	}
	return Civil(t.Year(), t.Month(), t.Day()), nil
}

func DateString(t time.Time) string {
	return fmt.Sprintf("%04d-%02d-%02d", t.Year(), int(t.Month()), t.Day())
}

// Today is the calendar date of now in the store timezone.
func Today(now time.Time, tz string) time.Time {
	loc, err := clock.Load(tz)
	if err != nil {
		loc, _ = clock.Load("Asia/Jakarta")
	}
	n := now.In(loc)
	return Civil(n.Year(), n.Month(), n.Day())
}

// MondayOf returns the Monday of the week containing t (as a civil date).
func MondayOf(t time.Time) time.Time {
	d := Civil(t.Year(), t.Month(), t.Day())
	delta := (int(d.Weekday()) + 6) % 7
	return d.AddDate(0, 0, -delta)
}

func DayLabel(t time.Time) string {
	d := Civil(t.Year(), t.Month(), t.Day())
	return fmt.Sprintf("%s, %d %s", dayShort[d.Weekday()], d.Day(), monthShort[d.Month()-1])
}

// Cutoff is the instant ordering for serviceDate closes, in the store zone.
// daysBefore 1 and clock "18:00" means the previous local day at 18:00.
// The returned instant is absolute. Ordering is open while now is strictly before it.
func Cutoff(serviceDate time.Time, tz string, daysBefore int, clockHM string) (time.Time, error) {
	if daysBefore < 0 || daysBefore > 14 {
		return time.Time{}, fmt.Errorf("bad cutoff days")
	}
	hh, mm, err := parseClock(clockHM)
	if err != nil {
		return time.Time{}, err
	}
	loc, err := clock.Load(tz)
	if err != nil {
		loc, _ = clock.Load("Asia/Jakarta")
	}
	y, m, d := serviceDate.Date()
	local := time.Date(y, m, d, hh, mm, 0, 0, loc).AddDate(0, 0, -daysBefore)
	return local, nil
}

// OrderingOpen reports whether now is still before the cutoff instant.
func OrderingOpen(now, cutoff time.Time) bool {
	return now.Before(cutoff)
}

func parseClock(s string) (int, int, error) {
	s = strings.TrimSpace(s)
	if len(s) >= 5 && s[2] == ':' {
		hh, err1 := strconv.Atoi(s[0:2])
		mm, err2 := strconv.Atoi(s[3:5])
		if err1 == nil && err2 == nil && hh >= 0 && hh <= 23 && mm >= 0 && mm <= 59 {
			return hh, mm, nil
		}
	}
	return 0, 0, fmt.Errorf("bad clock")
}

// NormalizeClock returns HH:MM or an error.
func NormalizeClock(s string) (string, error) {
	hh, mm, err := parseClock(s)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%02d:%02d", hh, mm), nil
}

// TplItem is one cell of a brand weekly template.
type TplItem struct {
	Weekday   int
	MealSlot  string
	ProductID uuid.UUID
	VariantID *uuid.UUID
	SortOrder int
	Quota     *int32
}

// Day is a persisted calendar row. Nil means "use the template".
type Day struct {
	Source string
	Closed bool
	Notes  string
}

// Item is a dish on a resolved date.
type Item struct {
	ProductID uuid.UUID
	VariantID *uuid.UUID
	MealSlot  string
	SortOrder int
	Quota     *int32
}

// Resolved is what customers and the kitchen see for one date.
type Resolved struct {
	Source string
	Closed bool
	Notes  string
	Items  []Item
}

// Resolve picks the override/copy row when one exists, otherwise the template weekday.
// A closed row hides the template for that date.
func Resolve(date time.Time, tpl []TplItem, day *Day, items []Item) Resolved {
	if day != nil {
		out := Resolved{Source: day.Source, Closed: day.Closed, Notes: day.Notes}
		if day.Closed {
			return out
		}
		out.Items = append([]Item(nil), items...)
		if out.Source == "" {
			out.Source = "override"
		}
		return out
	}
	wd := int(Civil(date.Year(), date.Month(), date.Day()).Weekday())
	var matched []Item
	for _, it := range tpl {
		if it.Weekday != wd {
			continue
		}
		matched = append(matched, Item{
			ProductID: it.ProductID,
			VariantID: it.VariantID,
			MealSlot:  it.MealSlot,
			SortOrder: it.SortOrder,
			Quota:     it.Quota,
		})
	}
	if len(matched) == 0 {
		return Resolved{Source: "empty"}
	}
	return Resolved{Source: "rotation", Items: matched}
}

// SameVariant treats nil and the zero UUID as "no variant".
func SameVariant(a, b *uuid.UUID) bool {
	az, bz := uuid.Nil, uuid.Nil
	if a != nil {
		az = *a
	}
	if b != nil {
		bz = *b
	}
	return az == bz
}

// ScheduleClosed reports a store-hours closure for a calendar date.
// The opening-schedule work should register a hook via SetScheduleHook.
// Until then every date is open at this layer (the menu day can still be closed).
var (
	scheduleMu     sync.RWMutex
	scheduleClosed func(storeID uuid.UUID, date time.Time) bool
)

func SetScheduleHook(fn func(storeID uuid.UUID, date time.Time) bool) {
	scheduleMu.Lock()
	scheduleClosed = fn
	scheduleMu.Unlock()
}

func ScheduledClosed(storeID uuid.UUID, date time.Time) bool {
	scheduleMu.RLock()
	fn := scheduleClosed
	scheduleMu.RUnlock()
	if fn == nil {
		return false
	}
	return fn(storeID, date)
}
