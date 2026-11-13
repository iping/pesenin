package service

import (
	"testing"
	"time"
)

func TestPaymentDeadlineUsesExactTime(t *testing.T) {
	now := time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)
	closes := time.Date(2026, 11, 15, 13, 0, 0, 0, time.UTC) // 20:00 WIB
	exact := time.Date(2026, 11, 14, 13, 0, 0, 0, time.UTC)  // 20:00 WIB the day before
	got := paymentExpiry(now, closes, nil, &exact, 24)
	if got == nil || !got.Equal(exact) {
		t.Fatalf("exact deadline %v", got)
	}
	fallback := paymentExpiry(now, closes, nil, nil, 24)
	want := now.Add(24 * time.Hour)
	if fallback == nil || !fallback.Equal(want) {
		t.Fatalf("fallback %v want %v", fallback, want)
	}
}

func TestSlotEndMustBeAfterStart(t *testing.T) {
	ready := time.Date(2026, 11, 16, 2, 0, 0, 0, time.UTC) // 09:00 WIB
	in := BatchIn{
		Title: "PO",
		OpensAt: time.Date(2026, 11, 14, 1, 0, 0, 0, time.UTC),
		ClosesAt: time.Date(2026, 11, 15, 13, 0, 0, 0, time.UTC),
		ReadyAt: ready,
		PickupSlots: []Slot{{Start: ready, End: ready}},
	}
	if err := normalizeBatch(&in); err == nil {
		t.Fatal("zero-length slot should fail")
	}
	in.PickupSlots[0].End = ready.Add(2 * time.Hour)
	if err := normalizeBatch(&in); err != nil {
		t.Fatal(err)
	}
}

func TestTimesSnapToFifteenMinutes(t *testing.T) {
	ready := time.Date(2026, 11, 16, 2, 7, 0, 0, time.UTC)
	in := BatchIn{
		Title: "PO",
		OpensAt: time.Date(2026, 11, 14, 1, 7, 0, 0, time.UTC),
		ClosesAt: time.Date(2026, 11, 15, 13, 8, 0, 0, time.UTC),
		ReadyAt: ready,
		PickupSlots: []Slot{{Start: ready, End: ready.Add(2*time.Hour + 7*time.Minute)}},
	}
	if err := normalizeBatch(&in); err != nil {
		t.Fatal(err)
	}
	if in.OpensAt.Minute()%15 != 0 || in.ClosesAt.Minute()%15 != 0 || in.PickupSlots[0].End.Minute()%15 != 0 {
		t.Fatalf("not snapped %#v", in)
	}
}
