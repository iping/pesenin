package authlimit

import (
	"testing"
	"time"
)

func TestAllowWindow(t *testing.T) {
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	l := New()
	l.now = func() time.Time { return now }
	if !l.Allow("login:ip:1", 2, time.Minute) || !l.Allow("login:ip:1", 2, time.Minute) {
		t.Fatal("first hits")
	}
	if l.Allow("login:ip:1", 2, time.Minute) {
		t.Fatal("third hit should block")
	}
	if !l.Allow("login:ip:2", 2, time.Minute) {
		t.Fatal("other key")
	}
	now = now.Add(time.Minute)
	if !l.Allow("login:ip:1", 2, time.Minute) {
		t.Fatal("window should reset")
	}
}

func TestFailureLockout(t *testing.T) {
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	l := New()
	l.now = func() time.Time { return now }
	for i := 0; i < 4; i++ {
		l.Fail("login:email:a@b.c", 5, 10*time.Minute)
		if l.Locked("login:email:a@b.c") {
			t.Fatalf("locked early at %d", i)
		}
	}
	l.Fail("login:email:a@b.c", 5, 10*time.Minute)
	if !l.Locked("login:email:a@b.c") {
		t.Fatal("expected lock")
	}
	if l.Locked("login:email:other") {
		t.Fatal("other key locked")
	}
	now = now.Add(10 * time.Minute)
	if l.Locked("login:email:a@b.c") {
		t.Fatal("lock should expire")
	}
	l.Clear("login:email:a@b.c")
	l.Fail("login:email:a@b.c", 5, 10*time.Minute)
	if l.Locked("login:email:a@b.c") {
		t.Fatal("clear should reset the counter")
	}
}
