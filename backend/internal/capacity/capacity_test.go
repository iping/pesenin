package capacity

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestMenuCheckerItemAndDay(t *testing.T) {
	q := int32(2)
	day := int32(3)
	dec := MenuChecker{}.Allow(context.Background(), DayDemand{
		StoreID: uuid.New(), Date: "2026-11-16", MealSlot: "lunch",
		DaySold: 2, DayLimit: &day,
		Lines: []Line{{Name: "Nasi", Qty: 2, Sold: 1, Quota: &q}},
	})
	if dec.OK || dec.Code != "quota" {
		t.Fatalf("quota %+v", dec)
	}
	dec = MenuChecker{}.Allow(context.Background(), DayDemand{
		StoreID: uuid.New(), Date: "2026-11-16",
		DaySold: 2, DayLimit: &day,
		Lines: []Line{{Name: "Nasi", Qty: 2, Sold: 0, Quota: &q}},
	})
	if dec.OK || dec.Code != "day_capacity" {
		t.Fatalf("day %+v", dec)
	}
	dec = MenuChecker{}.Allow(context.Background(), DayDemand{
		StoreID: uuid.New(), Date: "2026-11-16",
		DaySold: 0, DayLimit: &day,
		Lines: []Line{{Name: "Nasi", Qty: 2, Sold: 0, Quota: &q}},
	})
	if !dec.OK {
		t.Fatalf("allow %+v", dec)
	}
}

type stubChecker struct{ code string }

func (s stubChecker) Allow(context.Context, DayDemand) Decision {
	return Decision{Code: s.code, Message: "dari PO"}
}

func TestPOCapacityHook(t *testing.T) {
	prev := POCapacity
	t.Cleanup(func() { POCapacity = prev })
	POCapacity = stubChecker{code: "po_min"}
	dec := MenuChecker{}.Allow(context.Background(), DayDemand{
		Lines: []Line{{Name: "Nasi", Qty: 1}},
	})
	if dec.OK || dec.Code != "po_min" {
		t.Fatalf("%+v", dec)
	}
}
