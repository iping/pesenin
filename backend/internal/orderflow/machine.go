// Package orderflow is the order state machine. Payment and fulfilment are
// separate so a seller can keep cooking only after the transfer is confirmed.
package orderflow

// CanPay reports whether a seller may mark the order paid from this status.
func CanPay(from string) bool {
	return from == "pending" || from == "proof_submitted"
}

// CanUploadProof is allowed while the customer still owes the transfer.
func CanUploadProof(from string) bool {
	return from == "pending" || from == "proof_submitted"
}

// CanRejectProof sends a submitted proof back to pending so the customer can upload again.
func CanRejectProof(from string) bool {
	return from == "proof_submitted"
}

// CanExpire covers unpaid orders only.
func CanExpire(from string) bool {
	return from == "pending" || from == "proof_submitted"
}

// CanCancel is allowed before payment, and after payment when the caller
// still wants a refund window check.
func CanCancel(from string) bool {
	return from == "pending" || from == "proof_submitted" || from == "paid"
}

// CanFulfil only after the order is paid.
func CanFulfil(payment, from, to string) bool {
	if payment != "paid" {
		return false
	}
	switch from + ">" + to {
	case "not_ready>ready", "ready>completed", "not_ready>no_show", "ready>no_show":
		return true
	default:
		return false
	}
}

// ReleasesQuota when an unpaid order is expired or cancelled.
func ReleasesQuota(from string) bool {
	return from == "pending" || from == "proof_submitted"
}
