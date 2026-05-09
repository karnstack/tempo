package auth

import (
	"errors"
	"strings"
	"testing"
)

// fastParams keeps unit tests under a few ms each. The slow path
// (DefaultParams) is exercised once in TestHash_DefaultParams_RoundTrip.
var fastParams = Params{
	Memory:      8 * 1024,
	Iterations:  1,
	Parallelism: 1,
	SaltLen:     16,
	KeyLen:      32,
}

func TestHash_DefaultParams_RoundTrip(t *testing.T) {
	enc, err := Hash("hunter2")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if !strings.HasPrefix(enc, "$argon2id$v=19$m=65536,t=3,p=2$") {
		t.Fatalf("encoded prefix mismatch: %q", enc)
	}
	ok, err := Verify("hunter2", enc)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !ok {
		t.Fatal("Verify returned false for matching password")
	}
}

func TestHash_CustomParams_RoundTrip(t *testing.T) {
	enc, err := HashWithParams("correct horse battery staple", fastParams)
	if err != nil {
		t.Fatalf("HashWithParams: %v", err)
	}
	if !strings.Contains(enc, "m=8192,t=1,p=1") {
		t.Fatalf("encoded does not carry custom params: %q", enc)
	}
	ok, err := Verify("correct horse battery staple", enc)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !ok {
		t.Fatal("Verify returned false for matching password")
	}
}

func TestVerify_WrongPassword(t *testing.T) {
	enc, err := HashWithParams("hunter2", fastParams)
	if err != nil {
		t.Fatalf("HashWithParams: %v", err)
	}
	ok, err := Verify("nope", enc)
	if err != nil {
		t.Fatalf("Verify returned error for plain mismatch: %v", err)
	}
	if ok {
		t.Fatal("Verify returned true for wrong password")
	}
}

func TestVerify_TamperedHash(t *testing.T) {
	enc, err := HashWithParams("hunter2", fastParams)
	if err != nil {
		t.Fatalf("HashWithParams: %v", err)
	}
	// Flip the final character of the hash segment. The PHC string ends
	// with the base64 hash, so mutating the last byte produces a valid
	// base64 string that decodes to a different key.
	b := []byte(enc)
	last := b[len(b)-1]
	if last == 'A' {
		b[len(b)-1] = 'B'
	} else {
		b[len(b)-1] = 'A'
	}
	ok, err := Verify("hunter2", string(b))
	if err != nil {
		t.Fatalf("Verify returned error for tampered hash: %v", err)
	}
	if ok {
		t.Fatal("Verify returned true for tampered hash")
	}
}

func TestHash_EmptyPassword(t *testing.T) {
	_, err := Hash("")
	if !errors.Is(err, ErrEmptyPassword) {
		t.Fatalf("Hash(\"\") err = %v, want ErrEmptyPassword", err)
	}
	_, err = HashWithParams("", fastParams)
	if !errors.Is(err, ErrEmptyPassword) {
		t.Fatalf("HashWithParams(\"\", _) err = %v, want ErrEmptyPassword", err)
	}
}

func TestVerify_MalformedEncoded(t *testing.T) {
	good, err := HashWithParams("hunter2", fastParams)
	if err != nil {
		t.Fatalf("HashWithParams: %v", err)
	}

	cases := []struct {
		name    string
		encoded string
	}{
		{"empty", ""},
		{"wrong algorithm", strings.Replace(good, "$argon2id$", "$argon2i$", 1)},
		{"wrong version", strings.Replace(good, "$v=19$", "$v=18$", 1)},
		{"missing fields", "$argon2id$v=19$m=8192,t=1,p=1$onlyonesegment"},
		{"too many fields", good + "$extra"},
		{"bad params triple", strings.Replace(good, "m=8192,t=1,p=1", "m=abc,t=1,p=1", 1)},
		{"bad version", strings.Replace(good, "v=19", "v=xx", 1)},
		{"bad base64 salt", mutateSegment(t, good, 4, "!!notbase64!!")},
		{"bad base64 hash", mutateSegment(t, good, 5, "!!notbase64!!")},
		{"empty salt", mutateSegment(t, good, 4, "")},
		{"empty hash", mutateSegment(t, good, 5, "")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ok, err := Verify("hunter2", tc.encoded)
			if err == nil {
				t.Fatalf("Verify returned nil error for %s; ok=%v", tc.name, ok)
			}
			if ok {
				t.Fatalf("Verify returned true for malformed %s", tc.name)
			}
		})
	}
}

func TestHash_SaltRandomness(t *testing.T) {
	a, err := HashWithParams("hunter2", fastParams)
	if err != nil {
		t.Fatalf("HashWithParams: %v", err)
	}
	b, err := HashWithParams("hunter2", fastParams)
	if err != nil {
		t.Fatalf("HashWithParams: %v", err)
	}
	if a == b {
		t.Fatal("two Hash calls produced identical output; salt is not random")
	}
}

// mutateSegment returns encoded with the n-th $-delimited segment
// replaced by replacement. Indices match strings.Split output:
// 0=empty, 1=algo, 2=version, 3=params, 4=salt, 5=hash.
func mutateSegment(t *testing.T, encoded string, n int, replacement string) string {
	t.Helper()
	parts := strings.Split(encoded, "$")
	if n >= len(parts) {
		t.Fatalf("mutateSegment: index %d out of range for %q", n, encoded)
	}
	parts[n] = replacement
	return strings.Join(parts, "$")
}
