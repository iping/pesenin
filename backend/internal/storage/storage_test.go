package storage

import "testing"

func TestCacheControl(t *testing.T) {
	if got := CacheControl("products/abc/d1600-t480.webp"); got != "public, max-age=31536000, immutable, no-transform" {
		t.Fatal(got)
	}
	for _, key := range []string{"proofs/abc/f2400.webp", "topups/abc/f800-t480.webp"} {
		if got := CacheControl(key); got != "private, max-age=31536000, immutable, no-transform" {
			t.Fatalf("%s -> %s", key, got)
		}
	}
	if got := CacheControl("other/file.jpg"); got != "private, max-age=86400" {
		t.Fatal(got)
	}
}
