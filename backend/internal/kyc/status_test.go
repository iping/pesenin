package kyc

import "testing"

func TestPromoteContact(t *testing.T) {
	next, ok := PromoteContact(Unverified, true)
	if !ok || next != ContactVerified {
		t.Fatal(next, ok)
	}
	next, ok = PromoteContact(ContactVerified, false)
	if !ok || next != Unverified {
		t.Fatal(next, ok)
	}
	next, ok = PromoteContact(PendingReview, false)
	if !ok || next != Unverified {
		t.Fatal(next, ok)
	}
	if _, ok := PromoteContact(Verified, false); ok {
		t.Fatal("verified should stay")
	}
	if _, ok := PromoteContact(Suspended, true); ok {
		t.Fatal("suspended should stay")
	}
}

func TestReviewTransitions(t *testing.T) {
	if got, err := Submit(ContactVerified, true); err != nil || got != PendingReview {
		t.Fatal(got, err)
	}
	if _, err := Submit(Unverified, true); err == nil {
		t.Fatal("unverified submit")
	}
	if _, err := Submit(ContactVerified, false); err == nil {
		t.Fatal("submit without contact")
	}
	if got, err := Submit(Rejected, true); err != nil || got != PendingReview {
		t.Fatal(got, err)
	}
	if got, err := Approve(PendingReview, true); err != nil || got != Verified {
		t.Fatal(got, err)
	}
	if _, err := Approve(PendingReview, false); err == nil {
		t.Fatal("approve without contact")
	}
	if _, err := Approve(Suspended, true); err == nil {
		t.Fatal("approve suspended")
	}
	if got, err := Reject(Verified); err != nil || got != Rejected {
		t.Fatal(got, err)
	}
	if _, err := Reject(Unverified); err == nil {
		t.Fatal("reject unverified")
	}
	if got, err := Suspend(Verified); err != nil || got != Suspended {
		t.Fatal(got, err)
	}
	if _, err := Suspend(Suspended); err == nil {
		t.Fatal("double suspend")
	}
}

func TestCanTransact(t *testing.T) {
	if !CanTransact(Unverified, false, false) {
		t.Fatal("gate off")
	}
	if CanTransact(Unverified, true, true) {
		t.Fatal("unverified")
	}
	if CanTransact(ContactVerified, false, true) {
		t.Fatal("contact lapsed")
	}
	for _, st := range []string{ContactVerified, PendingReview, Verified} {
		if !CanTransact(st, true, true) {
			t.Fatal(st)
		}
	}
	if CanTransact(Rejected, true, true) || CanTransact(Suspended, true, true) {
		t.Fatal("blocked status")
	}
}
