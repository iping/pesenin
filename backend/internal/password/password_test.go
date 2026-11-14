package password

import (
	"strings"
	"testing"

	"golang.org/x/crypto/argon2"
)

func TestHashVerifiesAndUsesFreshSalt(t *testing.T) {
	a, err := Hash("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	b, err := Hash("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("expected a unique salt")
	}
	if !strings.HasPrefix(a, "$argon2id$v=19$m=19456,t=2,p=1$") {
		t.Fatal(a)
	}
	ok, rehash, err := Verify("correct horse battery staple", a)
	if err != nil || !ok || rehash {
		t.Fatalf("verify ok=%v rehash=%v err=%v", ok, rehash, err)
	}
	ok, rehash, err = Verify("wrong horse battery staple", a)
	if err != nil || ok || rehash {
		t.Fatalf("wrong password ok=%v rehash=%v err=%v", ok, rehash, err)
	}
	if _, _, err := Verify("x", "not-a-hash"); err == nil {
		t.Fatal("expected malformed hash")
	}
}

func TestVerifyDetectsParameterUpgrade(t *testing.T) {
	old := Params{Memory: 32, Time: 1, Threads: 1, KeyLen: 32, SaltLen: 16}
	phc, err := HashWith("naik-kelas-ya", old)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(phc, "m=32,t=1,p=1") {
		t.Fatal(phc)
	}
	ok, rehash, err := Verify("naik-kelas-ya", phc)
	if err != nil || !ok || !rehash {
		t.Fatalf("ok=%v rehash=%v err=%v", ok, rehash, err)
	}
	ok, _, err = Verify("bukan-ini", phc)
	if err != nil || ok {
		t.Fatalf("mismatch should fail, ok=%v err=%v", ok, err)
	}
	if argon2.Version != 19 {
		t.Fatalf("argon2 version %d", argon2.Version)
	}
}

func TestPolicy(t *testing.T) {
	if err := Check("short"); err == nil {
		t.Fatal("short")
	}
	if err := Check("password123"); err == nil {
		t.Fatal("common")
	}
	if err := Check("PASSWORD123"); err == nil {
		t.Fatal("common case")
	}
	long := strings.Repeat("a", MaxLen+1)
	if err := Check(long); err == nil {
		t.Fatal("long")
	}
	if err := Check("kucing-oranye"); err != nil {
		t.Fatal(err)
	}
	if err := Check("1234567"); err == nil {
		t.Fatal("seven")
	}
}

func TestDummyHash(t *testing.T) {
	a, err := Dummy()
	if err != nil {
		t.Fatal(err)
	}
	b, err := Dummy()
	if err != nil || a != b || !strings.HasPrefix(a, "$argon2id$") {
		t.Fatal(b, err)
	}
}
