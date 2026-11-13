// Package kyc is the brand verification state machine.
// Document review (KTP/NIB) is intentionally not a transition yet.
package kyc

import "errors"

const (
	Unverified      = "unverified"
	ContactVerified = "contact_verified"
	PendingReview   = "pending_review"
	Verified        = "verified"
	Rejected        = "rejected"
	Suspended       = "suspended"
)

// ErrTransition is an illegal status change.
var ErrTransition = errors.New("illegal kyc transition")

// PromoteContact moves a brand when the owner's email and WhatsApp change.
// Verified, rejected, and suspended stay put; the commerce gate checks contact separately.
func PromoteContact(status string, ready bool) (string, bool) {
	if ready && status == Unverified {
		return ContactVerified, true
	}
	if !ready && (status == ContactVerified || status == PendingReview) {
		return Unverified, true
	}
	return status, false
}

// Submit asks Office Pesenin to review a brand whose contact is already verified.
func Submit(status string, ready bool) (string, error) {
	if !ready {
		return status, ErrTransition
	}
	switch status {
	case ContactVerified, Rejected:
		return PendingReview, nil
	default:
		return status, ErrTransition
	}
}

// Approve marks a brand verified. Contact must still be verified.
func Approve(status string, ready bool) (string, error) {
	if !ready {
		return status, ErrTransition
	}
	switch status {
	case PendingReview, ContactVerified:
		return Verified, nil
	default:
		return status, ErrTransition
	}
}

// Reject sends a brand back to the seller with a reason.
func Reject(status string) (string, error) {
	switch status {
	case PendingReview, ContactVerified, Verified:
		return Rejected, nil
	default:
		return status, ErrTransition
	}
}

// Suspend blocks a brand from any other state.
func Suspend(status string) (string, error) {
	if status == Suspended {
		return status, ErrTransition
	}
	return Suspended, nil
}

// CanTransact is the paid-order and top-up gate.
// gate false leaves the check off (KYC_GATE=off).
func CanTransact(status string, ready, gate bool) bool {
	if !gate {
		return true
	}
	if !ready {
		return false
	}
	switch status {
	case ContactVerified, PendingReview, Verified:
		return true
	default:
		return false
	}
}
