// Package clock formats store-local instants and snaps them to 15 minutes.
package clock

import (
	"fmt"
	"time"
	_ "time/tzdata" // Vercel's Go runtime has no system zoneinfo; WIB/WITA/WIT stay correct.
)

// Snap15 moves t onto a 15-minute boundary (:00, :15, :30, :45), dropping seconds.
func Snap15(t time.Time) time.Time {
	t = t.Truncate(time.Minute)
	rem := t.Minute() % 15
	if rem == 0 {
		return t
	}
	if rem < 8 {
		return t.Add(-time.Duration(rem) * time.Minute)
	}
	return t.Add(time.Duration(15-rem) * time.Minute)
}

func Zone(tz string) string {
	switch tz {
	case "Asia/Makassar", "Asia/Ujung_Pandang":
		return "WITA"
	case "Asia/Jayapura":
		return "WIT"
	default:
		return "WIB"
	}
}

func loc(tz string) *time.Location {
	l, err := time.LoadLocation(tz)
	if err != nil || tz == "" {
		l, _ = time.LoadLocation("Asia/Jakarta")
	}
	return l
}

var days = []string{"Min", "Sen", "Sel", "Rab", "Kam", "Jum", "Sab"}
var months = []string{"Jan", "Feb", "Mar", "Apr", "Mei", "Jun", "Jul", "Agu", "Sep", "Okt", "Nov", "Des"}

// When renders "Sab, 14 Nov 2026 20:00 WIB".
func When(t time.Time, tz string) string {
	tt := t.In(loc(tz))
	return fmt.Sprintf("%s, %d %s %d %02d:%02d %s", days[tt.Weekday()], tt.Day(), months[tt.Month()-1], tt.Year(), tt.Hour(), tt.Minute(), Zone(tz))
}

// Slot renders one window. Same local day shares the date: "Min, 15 Nov 2026 10:00–12:00 WIB".
func Slot(start, end time.Time, tz string) string {
	a := start.In(loc(tz))
	b := end.In(loc(tz))
	label := Zone(tz)
	if a.Year() == b.Year() && a.YearDay() == b.YearDay() {
		return fmt.Sprintf("%s, %d %s %d %02d:%02d–%02d:%02d %s", days[a.Weekday()], a.Day(), months[a.Month()-1], a.Year(), a.Hour(), a.Minute(), b.Hour(), b.Minute(), label)
	}
	return When(start, tz) + " – " + When(end, tz)
}
