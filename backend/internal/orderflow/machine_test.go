package orderflow

import "testing"

func TestPaymentTransitions(t *testing.T) {
	if !CanPay("pending") || !CanPay("proof_submitted") {
		t.Fatal("paid path")
	}
	if CanPay("paid") || CanPay("expired") || CanPay("cancelled") {
		t.Fatal("paid path should be closed")
	}
	if !CanRejectProof("proof_submitted") || CanRejectProof("pending") {
		t.Fatal("reject")
	}
	if !CanExpire("pending") || !CanExpire("proof_submitted") || CanExpire("paid") {
		t.Fatal("expire")
	}
	if !CanCancel("paid") || !CanCancel("pending") || CanCancel("expired") {
		t.Fatal("cancel")
	}
	if !ReleasesQuota("pending") || ReleasesQuota("paid") {
		t.Fatal("quota")
	}
}

func TestFulfilmentOnlyAfterPaid(t *testing.T) {
	if CanFulfil("pending", "not_ready", "ready") {
		t.Fatal("cannot ready before paid")
	}
	if !CanFulfil("paid", "not_ready", "ready") || !CanFulfil("paid", "ready", "completed") {
		t.Fatal("happy path")
	}
	if CanFulfil("paid", "completed", "ready") || CanFulfil("paid", "not_ready", "completed") {
		t.Fatal("skip or rewind")
	}
	if !CanFulfil("paid", "ready", "no_show") {
		t.Fatal("no-show")
	}
}
