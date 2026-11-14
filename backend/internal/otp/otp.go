// Package otp builds short-lived numeric codes and stores only a salted hash.
package otp

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
)

const Digits = 6

// Code is what we persist. Plain is returned once to the sender.
type Code struct {
	Plain string
	Salt  string
	Hash  string
}

// New returns a 6-digit code and the salted SHA-256 we store.
func New() (Code, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return Code{}, err
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return Code{}, err
	}
	plain := fmt.Sprintf("%06d", n.Int64())
	s := base64.RawStdEncoding.EncodeToString(salt)
	return Code{Plain: plain, Salt: s, Hash: Hash(s, plain)}, nil
}

// Hash is SHA-256(salt || ":" || code), hex encoded.
func Hash(salt, plain string) string {
	sum := sha256.Sum256([]byte(salt + ":" + plain))
	return hex.EncodeToString(sum[:])
}

// Match compares in constant time. Both sides are hex hashes of equal length
// when the row was written by New.
func Match(salt, plain, hash string) bool {
	got := Hash(salt, plain)
	if len(got) != len(hash) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(hash)) == 1
}

// ErrMismatch, ErrExpired, and ErrAttempts are check results.
var (
	ErrMismatch = errors.New("mismatch")
	ErrExpired  = errors.New("expired")
	ErrAttempts = errors.New("attempts")
)
