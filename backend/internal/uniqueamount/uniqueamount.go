// Package uniqueamount picks the rupiah suffix that makes a transfer total
// unique for one bank account. Sellers match the last digits in their bank app.
package uniqueamount

import (
	"crypto/rand"
	"errors"
	"math/big"
)

// ErrExhausted means every suffix in 1..999 and 1001..1999 is taken.
var ErrExhausted = errors.New("no free unique-amount suffix")

// Pick chooses a random free suffix. Suffixes ending in 00 are skipped so the
// total does not look like a round number the seller typed by hand.
// taken holds totals that are still reserved (active, or expired/cancelled
// within 48 hours).
func Pick(base int64, taken map[int64]struct{}) (suffix int32, total int64, err error) {
	if s, tot, ok := pickRange(base, 1, 999, taken); ok {
		return s, tot, nil
	}
	if s, tot, ok := pickRange(base, 1001, 1999, taken); ok {
		return s, tot, nil
	}
	return 0, 0, ErrExhausted
}

func pickRange(base, lo, hi int64, taken map[int64]struct{}) (int32, int64, bool) {
	n := int(hi - lo + 1)
	order := perm(n)
	for _, i := range order {
		s := lo + int64(i)
		if s%100 == 0 {
			continue
		}
		tot := base + s
		if _, used := taken[tot]; used {
			continue
		}
		return int32(s), tot, true
	}
	return 0, 0, false
}

func perm(n int) []int {
	idx := make([]int, n)
	for i := range idx {
		idx[i] = i
	}
	for i := n - 1; i > 0; i-- {
		j := int(cryptoInt(int64(i + 1)))
		idx[i], idx[j] = idx[j], idx[i]
	}
	return idx
}

func cryptoInt(max int64) int64 {
	if max <= 0 {
		return 0
	}
	n, err := rand.Int(rand.Reader, big.NewInt(max))
	if err != nil {
		return 0
	}
	return n.Int64()
}
