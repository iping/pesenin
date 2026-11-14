// Package password hashes passwords with Argon2id and checks a short policy.
// Hashes are PHC strings so the parameters can change later.
package password

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"

	"golang.org/x/crypto/argon2"
)

// Current is the OWASP Argon2id baseline: 19 MiB, 2 iterations, parallelism 1.
var Current = Params{
	Memory:  19 * 1024,
	Time:    2,
	Threads: 1,
	KeyLen:  32,
	SaltLen: 16,
}

// Params are the Argon2id settings stored inside a PHC hash.
type Params struct {
	Memory  uint32
	Time    uint32
	Threads uint8
	KeyLen  uint32
	SaltLen uint32
}

// ErrMalformed is a hash this package did not encode.
var ErrMalformed = errors.New("malformed password hash")

// Hash returns a PHC string for pw using Current.
func Hash(pw string) (string, error) {
	return HashWith(pw, Current)
}

// HashWith encodes pw with p. Callers pass the raw password and do not log it.
func HashWith(pw string, p Params) (string, error) {
	if p.SaltLen < 16 || p.KeyLen < 16 || p.Time < 1 || p.Threads < 1 || p.Memory < 8*uint32(p.Threads) {
		return "", errors.New("invalid argon2 params")
	}
	salt := make([]byte, p.SaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	sum := argon2.IDKey([]byte(pw), salt, p.Time, p.Memory, p.Threads, p.KeyLen)
	return encode(p, salt, sum), nil
}

// Verify checks pw against a PHC string.
// needsRehash is true only when pw matches and the stored parameters differ from Current.
func Verify(pw, phc string) (ok bool, needsRehash bool, err error) {
	p, salt, want, err := decode(phc)
	if err != nil {
		return false, false, err
	}
	sum := argon2.IDKey([]byte(pw), salt, p.Time, p.Memory, p.Threads, uint32(len(want)))
	if subtle.ConstantTimeCompare(sum, want) != 1 {
		return false, false, nil
	}
	rehash := p.Memory != Current.Memory || p.Time != Current.Time || p.Threads != Current.Threads || uint32(len(want)) != Current.KeyLen
	return true, rehash, nil
}

var (
	dummyOnce sync.Once
	dummyPHC  string
	dummyErr  error
)

// Dummy is a real Argon2id hash used when the account has no password,
// so the missing-account path still runs the KDF.
func Dummy() (string, error) {
	dummyOnce.Do(func() {
		buf := make([]byte, 32)
		if _, dummyErr = rand.Read(buf); dummyErr != nil {
			return
		}
		dummyPHC, dummyErr = Hash(base64.RawStdEncoding.EncodeToString(buf))
	})
	return dummyPHC, dummyErr
}

func encode(p Params, salt, sum []byte) string {
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, p.Memory, p.Time, p.Threads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(sum))
}

func decode(phc string) (Params, []byte, []byte, error) {
	parts := strings.Split(phc, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return Params{}, nil, nil, ErrMalformed
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return Params{}, nil, nil, ErrMalformed
	}
	var p Params
	var threads uint32
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.Memory, &p.Time, &threads); err != nil || threads < 1 || threads > 255 {
		return Params{}, nil, nil, ErrMalformed
	}
	p.Threads = uint8(threads)
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) < 8 {
		return Params{}, nil, nil, ErrMalformed
	}
	sum, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(sum) < 16 {
		return Params{}, nil, nil, ErrMalformed
	}
	if p.Time < 1 || p.Memory < 8*uint32(p.Threads) {
		return Params{}, nil, nil, ErrMalformed
	}
	return p, salt, sum, nil
}
