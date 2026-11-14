package otp

import "testing"

func TestNewCodeIsSixDigitsAndHashed(t *testing.T) {
	c, err := New()
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Plain) != Digits {
		t.Fatal(c.Plain)
	}
	for _, r := range c.Plain {
		if r < '0' || r > '9' {
			t.Fatal(c.Plain)
		}
	}
	if c.Hash == c.Plain || c.Hash == "" || c.Salt == "" {
		t.Fatal("hash or salt missing")
	}
	if !Match(c.Salt, c.Plain, c.Hash) {
		t.Fatal("match")
	}
	if Match(c.Salt, "000000", c.Hash) && c.Plain != "000000" {
		t.Fatal("false match")
	}
	other, err := New()
	if err != nil {
		t.Fatal(err)
	}
	if Match(other.Salt, c.Plain, c.Hash) {
		t.Fatal("salt should change the hash")
	}
}
