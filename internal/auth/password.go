package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Params controls the cost of a single Argon2id hash.
type Params struct {
	Memory      uint32 // KiB
	Iterations  uint32
	Parallelism uint8
	SaltLen     uint32
	KeyLen      uint32
}

// DefaultParams follows OWASP 2024 guidance for Argon2id: 64 MiB memory,
// 3 iterations, 2 lanes. Heavy enough to make offline cracking expensive
// without making the admin login flow feel slow.
var DefaultParams = Params{
	Memory:      64 * 1024,
	Iterations:  3,
	Parallelism: 2,
	SaltLen:     16,
	KeyLen:      32,
}

// ErrEmptyPassword is returned by Hash when the input is empty. We never
// store an empty admin password — surface it as an error so the caller has
// to reject it explicitly.
var ErrEmptyPassword = errors.New("auth: password is empty")

// Hash returns a PHC-formatted Argon2id hash of password using DefaultParams.
func Hash(password string) (string, error) {
	return HashWithParams(password, DefaultParams)
}

// HashWithParams is Hash with caller-supplied cost. Useful for tests and
// future tuning. Production code should call Hash.
func HashWithParams(password string, p Params) (string, error) {
	if password == "" {
		return "", ErrEmptyPassword
	}
	salt := make([]byte, p.SaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("auth: read salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, p.Iterations, p.Memory, p.Parallelism, p.KeyLen)
	enc := base64.RawStdEncoding
	return fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, p.Memory, p.Iterations, p.Parallelism,
		enc.EncodeToString(salt), enc.EncodeToString(key),
	), nil
}

// Verify reports whether password matches the PHC-encoded Argon2id hash.
// Returns (false, nil) for a clean mismatch; (false, err) for malformed
// input. Uses subtle.ConstantTimeCompare so the timing channel doesn't
// leak partial matches.
func Verify(password, encoded string) (bool, error) {
	p, salt, key, err := decode(encoded)
	if err != nil {
		return false, err
	}
	candidate := argon2.IDKey([]byte(password), salt, p.Iterations, p.Memory, p.Parallelism, p.KeyLen)
	if subtle.ConstantTimeCompare(candidate, key) == 1 {
		return true, nil
	}
	return false, nil
}

func decode(encoded string) (Params, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	// "$argon2id$v=19$m=…,t=…,p=…$<salt>$<hash>" splits into 6 parts with
	// an empty leading element.
	if len(parts) != 6 {
		return Params{}, nil, nil, errors.New("auth: malformed argon2 hash: wrong field count")
	}
	if parts[1] != "argon2id" {
		return Params{}, nil, nil, fmt.Errorf("auth: unsupported algorithm %q (want argon2id)", parts[1])
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return Params{}, nil, nil, fmt.Errorf("auth: parse version: %w", err)
	}
	if version != argon2.Version {
		return Params{}, nil, nil, fmt.Errorf("auth: unsupported argon2 version %d (want %d)", version, argon2.Version)
	}
	var p Params
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.Memory, &p.Iterations, &p.Parallelism); err != nil {
		return Params{}, nil, nil, fmt.Errorf("auth: parse params: %w", err)
	}
	enc := base64.RawStdEncoding
	salt, err := enc.DecodeString(parts[4])
	if err != nil {
		return Params{}, nil, nil, fmt.Errorf("auth: decode salt: %w", err)
	}
	if len(salt) == 0 {
		return Params{}, nil, nil, errors.New("auth: empty salt")
	}
	key, err := enc.DecodeString(parts[5])
	if err != nil {
		return Params{}, nil, nil, fmt.Errorf("auth: decode hash: %w", err)
	}
	if len(key) == 0 {
		return Params{}, nil, nil, errors.New("auth: empty hash")
	}
	p.SaltLen = uint32(len(salt))
	p.KeyLen = uint32(len(key))
	return p, salt, key, nil
}
