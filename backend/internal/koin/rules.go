// Package koin holds the wallet rules that do not need a database:
// the admin fee, how many koin a top-up credits, and the grace floor.
package koin

import "time"

// Fee is whole rupiah. Percent is basis points (100 bps = 1%).
// P1 uses a fixed fee; percent, min and max stay in the versioned config.
func Fee(amount, fixed int64, percentBps int, minFee int64, maxFee *int64) int64 {
	variable := int64(0)
	if percentBps > 0 && amount > 0 {
		variable = ceilDiv(amount*int64(percentBps), 10000)
	}
	fee := fixed + variable
	if fee < minFee {
		fee = minFee
	}
	if maxFee != nil && fee > *maxFee {
		fee = *maxFee
	}
	if fee < 0 {
		return 0
	}
	return fee
}

func ceilDiv(a, b int64) int64 {
	if a <= 0 || b <= 0 {
		return 0
	}
	return (a + b - 1) / b
}

// Credit is the koin written on approval.
// Without a corrected amount the seller receives the top-up plus the unique
// suffix they actually transferred. The admin fee is not converted to koin.
// A corrected received amount credits received minus the snapshotted fee.
func Credit(amount, suffix, fee int64, received *int64) int64 {
	if received == nil {
		return amount + suffix
	}
	k := *received - fee
	if k < 0 {
		return 0
	}
	return k
}

// SoftLocked is true at or below the grace floor. Order debits still post.
func SoftLocked(balance, floor int64) bool {
	return balance <= floor
}

// LowBalance warns the seller. The floor itself has its own banner.
func LowBalance(balance, threshold, floor int64) bool {
	return balance < threshold && balance > floor
}

// RefundWindow is 24 hours after paid_at.
func RefundWindow(paidAt, now time.Time) bool {
	if paidAt.IsZero() {
		return false
	}
	return !now.After(paidAt.Add(24 * time.Hour))
}

// ValidCustomAmount checks the platform top-up bounds.
func ValidCustomAmount(amount, min, max, multiple int64) bool {
	if amount < min || amount > max {
		return false
	}
	if multiple <= 0 {
		return false
	}
	return amount%multiple == 0
}
