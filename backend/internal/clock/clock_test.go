package clock

import (
	"testing"
	"time"
)

func TestFormatJakarta(t *testing.T) {
	// 2026-11-14 13:00 UTC is 20:00 WIB, a Saturday.
	at := time.Date(2026, 11, 14, 13, 0, 0, 0, time.UTC)
	if got := When(at, "Asia/Jakarta"); got != "Sab, 14 Nov 2026 20:00 WIB" {
		t.Fatal(got)
	}
	end := at.Add(2 * time.Hour)
	// Sunday 15 Nov 10:00–12:00 WIB = 15 Nov 03:00–05:00 UTC
	start := time.Date(2026, 11, 15, 3, 0, 0, 0, time.UTC)
	if got := Slot(start, start.Add(2*time.Hour), "Asia/Jakarta"); got != "Min, 15 Nov 2026 10:00–12:00 WIB" {
		t.Fatal(got)
	}
	if got := Zone("Asia/Makassar"); got != "WITA" {
		t.Fatal(got)
	}
	_ = end
}

func TestSnap15(t *testing.T) {
	in := time.Date(2026, 11, 14, 13, 7, 40, 0, time.UTC)
	got := Snap15(in)
	if got.Minute() != 0 || got.Second() != 0 {
		t.Fatal(got)
	}
	up := Snap15(time.Date(2026, 11, 14, 13, 8, 0, 0, time.UTC))
	if up.Minute() != 15 {
		t.Fatal(up)
	}
}
