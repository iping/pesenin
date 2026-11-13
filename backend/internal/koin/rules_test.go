package koin

import (
	"testing"
	"time"
)

func TestFeeFixedOnly(t *testing.T) {
	if got := Fee(50000, 1000, 0, 0, nil); got != 1000 {
		t.Fatal(got)
	}
	max := int64(500)
	if got := Fee(50000, 1000, 0, 0, &max); got != 500 {
		t.Fatal(got)
	}
	if got := Fee(50000, 100, 0, 250, nil); got != 250 {
		t.Fatal(got)
	}
}

func TestFeePercent(t *testing.T) {
	// 1% of 50.000 = 500, plus fixed 0.
	if got := Fee(50000, 0, 100, 0, nil); got != 500 {
		t.Fatal(got)
	}
	// ceil(1 * 1 / 10000) waits until the product crosses a rupiah.
	if got := Fee(1, 0, 1, 0, nil); got != 1 {
		t.Fatal(got)
	}
}

func TestCredit(t *testing.T) {
	if got := Credit(50000, 37, 1000, nil); got != 50037 {
		t.Fatalf("snapshot credit %d", got)
	}
	received := int64(51037)
	if got := Credit(50000, 37, 1000, &received); got != 50037 {
		t.Fatalf("corrected matching total %d", got)
	}
	short := int64(40000)
	if got := Credit(50000, 37, 1000, &short); got != 39000 {
		t.Fatalf("short payment %d", got)
	}
	tiny := int64(500)
	if got := Credit(50000, 37, 1000, &tiny); got != 0 {
		t.Fatal(got)
	}
}

func TestFloorAndWindow(t *testing.T) {
	if !SoftLocked(-5000, -5000) || !SoftLocked(-5001, -5000) || SoftLocked(-4999, -5000) {
		t.Fatal("floor")
	}
	if !LowBalance(499, 500, -5000) || LowBalance(-5000, 500, -5000) || LowBalance(500, 500, -5000) {
		t.Fatal("low")
	}
	paid := time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)
	if !RefundWindow(paid, paid.Add(24*time.Hour)) {
		t.Fatal("edge of window should refund")
	}
	if RefundWindow(paid, paid.Add(24*time.Hour+time.Second)) {
		t.Fatal("after window")
	}
	if !ValidCustomAmount(25000, 25000, 1000000, 1000) || ValidCustomAmount(37500, 25000, 1000000, 1000) {
		t.Fatal("custom")
	}
}
