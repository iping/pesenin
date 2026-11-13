package uniqueamount

import "testing"

func TestPickSkipsRoundSuffixesAndCollisions(t *testing.T) {
	base := int64(45000)
	taken := map[int64]struct{}{}
	seen := map[int32]struct{}{}
	for i := 0; i < 40; i++ {
		suffix, total, err := Pick(base, taken)
		if err != nil {
			t.Fatal(err)
		}
		if suffix%100 == 0 {
			t.Fatalf("suffix %d ends in 00", suffix)
		}
		if total != base+int64(suffix) {
			t.Fatalf("total %d != base+suffix", total)
		}
		if suffix < 1 || (suffix > 999 && suffix < 1001) || suffix > 1999 {
			t.Fatalf("suffix %d out of range", suffix)
		}
		taken[total] = struct{}{}
		seen[suffix] = struct{}{}
	}
	if len(seen) < 30 {
		t.Fatalf("expected a spread of suffixes, got %d unique", len(seen))
	}
}

func TestPickWidensWhenPrimaryRangeIsFull(t *testing.T) {
	base := int64(10000)
	taken := map[int64]struct{}{}
	for s := int64(1); s <= 999; s++ {
		if s%100 == 0 {
			continue
		}
		taken[base+s] = struct{}{}
	}
	suffix, total, err := Pick(base, taken)
	if err != nil {
		t.Fatal(err)
	}
	if suffix < 1001 || suffix > 1999 || suffix%100 == 0 {
		t.Fatalf("expected widened suffix, got %d", suffix)
	}
	if _, used := taken[total]; used {
		t.Fatal("picked a taken total")
	}
}

func TestPickExhausted(t *testing.T) {
	base := int64(0)
	taken := map[int64]struct{}{}
	for s := int64(1); s <= 1999; s++ {
		if s%100 == 0 || (s > 999 && s < 1001) {
			continue
		}
		taken[base+s] = struct{}{}
	}
	if _, _, err := Pick(base, taken); err != ErrExhausted {
		t.Fatalf("got %v", err)
	}
}
