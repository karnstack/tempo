package secret_test

import (
	"bytes"
	"crypto/rand"
	"errors"
	"testing"

	"github.com/karnstack/tempo/internal/secret"
)

func newKey(t *testing.T) []byte {
	t.Helper()
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	return k
}

func TestNewBox_RejectsShortKey(t *testing.T) {
	if _, err := secret.NewBox(make([]byte, 16)); err == nil {
		t.Fatal("expected error for 16-byte key")
	}
}

func TestNewBox_RejectsLongKey(t *testing.T) {
	if _, err := secret.NewBox(make([]byte, 64)); err == nil {
		t.Fatal("expected error for 64-byte key")
	}
}

func TestEncryptDecrypt_RoundTrip(t *testing.T) {
	b, err := secret.NewBox(newKey(t))
	if err != nil {
		t.Fatalf("NewBox: %v", err)
	}
	plain := []byte("ghp_secrettokenvalue1234567890")
	ct, err := b.Encrypt(plain)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	got, err := b.Decrypt(ct)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if !bytes.Equal(got, plain) {
		t.Errorf("decrypted = %q, want %q", got, plain)
	}
}

func TestEncrypt_DistinctNonces(t *testing.T) {
	b, err := secret.NewBox(newKey(t))
	if err != nil {
		t.Fatalf("NewBox: %v", err)
	}
	a, err := b.Encrypt([]byte("same"))
	if err != nil {
		t.Fatalf("Encrypt a: %v", err)
	}
	c, err := b.Encrypt([]byte("same"))
	if err != nil {
		t.Fatalf("Encrypt c: %v", err)
	}
	if bytes.Equal(a, c) {
		t.Fatal("two encrypts of same plaintext produced identical ciphertext (nonce reuse?)")
	}
}

func TestDecrypt_TamperFails(t *testing.T) {
	b, err := secret.NewBox(newKey(t))
	if err != nil {
		t.Fatalf("NewBox: %v", err)
	}
	ct, err := b.Encrypt([]byte("hello"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	ct[len(ct)-1] ^= 0xff // flip a byte in the auth tag
	if _, err := b.Decrypt(ct); err == nil {
		t.Fatal("expected tamper to fail decrypt")
	}
}

func TestDecrypt_WrongKey(t *testing.T) {
	b1, err := secret.NewBox(newKey(t))
	if err != nil {
		t.Fatalf("NewBox b1: %v", err)
	}
	b2, err := secret.NewBox(newKey(t))
	if err != nil {
		t.Fatalf("NewBox b2: %v", err)
	}
	ct, err := b1.Encrypt([]byte("hello"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if _, err := b2.Decrypt(ct); err == nil {
		t.Fatal("expected wrong-key decrypt to fail")
	}
}

func TestDecrypt_TooShort(t *testing.T) {
	b, err := secret.NewBox(newKey(t))
	if err != nil {
		t.Fatalf("NewBox: %v", err)
	}
	if _, err := b.Decrypt([]byte{1, 2, 3}); !errors.Is(err, secret.ErrCipherTooShort) {
		t.Fatalf("err = %v, want ErrCipherTooShort", err)
	}
}

func TestEncryptDecrypt_EmptyPlaintext(t *testing.T) {
	b, err := secret.NewBox(newKey(t))
	if err != nil {
		t.Fatalf("NewBox: %v", err)
	}
	ct, err := b.Encrypt(nil)
	if err != nil {
		t.Fatalf("Encrypt(nil): %v", err)
	}
	got, err := b.Decrypt(ct)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %q, want empty", got)
	}
}
