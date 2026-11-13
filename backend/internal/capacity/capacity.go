// Package capacity is the daily-menu gate for portions.
//
// Item quotas and the optional per-day cap are enforced here. PO min, max,
// and shared kitchen capacity are owned by parallel work: set POCapacity
// when that checker exists and MenuChecker will call it first.
package capacity

import (
	"context"
	"strconv"

	"github.com/google/uuid"
)

// Line is one menu item the customer wants to add.
type Line struct {
	Name  string
	Qty   int32
	Sold  int32
	Quota *int32
}

// DayDemand is a single service date (and the slot the lines belong to).
type DayDemand struct {
	StoreID  uuid.UUID
	Date     string
	MealSlot string
	DaySold  int32
	DayLimit *int32
	Lines    []Line
}

// Decision is a yes, or a stable error code plus Indonesian copy.
type Decision struct {
	OK      bool
	Code    string
	Message string
}

// Checker reports whether a demand fits.
type Checker interface {
	Allow(ctx context.Context, d DayDemand) Decision
}

// POCapacity is the hook for PO min/max/capacity.
// Leave it nil until that package can answer DayDemand.
// Do not set it to MenuChecker (that would recurse).
var POCapacity Checker

// Default is what daily-menu ordering calls.
var Default Checker = MenuChecker{}

// MenuChecker enforces item quotas and the optional day cap,
// after POCapacity when one is registered.
type MenuChecker struct{}

func (MenuChecker) Allow(ctx context.Context, d DayDemand) Decision {
	if POCapacity != nil {
		if _, isMenu := POCapacity.(MenuChecker); !isMenu {
			if dec := POCapacity.Allow(ctx, d); !dec.OK {
				return dec
			}
		}
	}
	var add int32
	for _, ln := range d.Lines {
		if ln.Qty <= 0 {
			return Decision{Code: "bad_qty", Message: "Jumlah pesanan belum pas."}
		}
		add += ln.Qty
		if ln.Quota != nil && ln.Sold+ln.Qty > *ln.Quota {
			left := *ln.Quota - ln.Sold
			if left < 0 {
				left = 0
			}
			return Decision{Code: "quota", Message: ln.Name + " sisa " + strconv.Itoa(int(left)) + "."}
		}
	}
	if d.DayLimit != nil && d.DaySold+add > *d.DayLimit {
		return Decision{Code: "day_capacity", Message: "Kuota hari ini sudah penuh."}
	}
	return Decision{OK: true}
}
