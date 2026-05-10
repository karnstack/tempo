// Package secret hosts symmetric encryption helpers used to protect
// at-rest credentials (today: GitHub PATs). The key is a 32-byte symmetric
// key derived from TEMPO_SECRET; the wire format is nonce ‖ ciphertext.
package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
)

const keyLen = 32 // AES-256

// Box wraps a 32-byte AES key and exposes Encrypt/Decrypt for short blobs.
// Safe for concurrent use — the underlying cipher is constructed once.
type Box struct {
	gcm cipher.AEAD
}

// NewBox validates the key length and constructs the AEAD. The key is
// retained only inside the cipher block; callers can zero their copy
// after this returns.
func NewBox(key []byte) (*Box, error) {
	if len(key) != keyLen {
		return nil, fmt.Errorf("secret: key must be %d bytes, got %d", keyLen, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("secret: aes cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("secret: gcm: %w", err)
	}
	return &Box{gcm: gcm}, nil
}

// Encrypt returns nonce ‖ ciphertext. A fresh random nonce is generated
// per call so the same plaintext encrypts to different bytes each time.
func (b *Box) Encrypt(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, b.gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("secret: random nonce: %w", err)
	}
	ct := b.gcm.Seal(nil, nonce, plaintext, nil)
	out := make([]byte, 0, len(nonce)+len(ct))
	out = append(out, nonce...)
	out = append(out, ct...)
	return out, nil
}

// ErrCipherTooShort is returned when the input cannot possibly hold a
// nonce + at least an empty AEAD tag. Distinct from auth-failure so tests
// can tell them apart.
var ErrCipherTooShort = errors.New("secret: ciphertext too short")

// Decrypt splits the input into nonce + ciphertext and returns the
// authenticated plaintext. Returns an error on tamper or wrong key.
func (b *Box) Decrypt(blob []byte) ([]byte, error) {
	ns := b.gcm.NonceSize()
	if len(blob) < ns+b.gcm.Overhead() {
		return nil, ErrCipherTooShort
	}
	nonce, ct := blob[:ns], blob[ns:]
	pt, err := b.gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, fmt.Errorf("secret: open: %w", err)
	}
	return pt, nil
}
