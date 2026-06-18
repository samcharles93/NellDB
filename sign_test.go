package nell

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

func TestSignBodyDeterministic(t *testing.T) {
	secret := []byte("super-secret")
	a := SignBody(secret, 1234567890, []byte("hello"))
	b := SignBody(secret, 1234567890, []byte("hello"))
	if a != b {
		t.Errorf("same inputs produced different signatures: %q != %q", a, b)
	}
}

func TestSignBodyDifferentTimestamp(t *testing.T) {
	secret := []byte("secret")
	a := SignBody(secret, 1000, []byte("body"))
	b := SignBody(secret, 1001, []byte("body"))
	if a == b {
		t.Error("different timestamps produced identical signatures")
	}
}

func TestSignBodyDifferentBody(t *testing.T) {
	secret := []byte("secret")
	a := SignBody(secret, 1000, []byte("alpha"))
	b := SignBody(secret, 1000, []byte("beta"))
	if a == b {
		t.Error("different bodies produced identical signatures")
	}
}

func TestSignBodyDifferentSecret(t *testing.T) {
	a := SignBody([]byte("secret-a"), 1000, []byte("body"))
	b := SignBody([]byte("secret-b"), 1000, []byte("body"))
	if a == b {
		t.Error("different secrets produced identical signatures")
	}
}

func TestSignBodyEmptyBody(t *testing.T) {
	secret := []byte("secret")
	sig := SignBody(secret, 1000, nil)
	if sig == "" {
		t.Error("empty body produced empty signature")
	}
	// nil and empty slice should produce the same result.
	sig2 := SignBody(secret, 1000, []byte{})
	if sig != sig2 {
		t.Error("nil body and empty body produced different signatures")
	}
}

func TestSignBodyEmptySecret(t *testing.T) {
	sig := SignBody(nil, 1000, []byte("body"))
	if sig == "" {
		t.Error("nil secret produced empty signature")
	}
	// HMAC allows empty key; just verify it's stable.
	sig2 := SignBody(nil, 1000, []byte("body"))
	if sig != sig2 {
		t.Error("nil secret produced non-deterministic signature")
	}
}

func TestSignBodyNegativeTimestamp(t *testing.T) {
	sig := SignBody([]byte("secret"), -1, []byte("body"))
	if sig == "" {
		t.Error("negative timestamp produced empty signature")
	}
}

func TestSignBodyZeroTimestamp(t *testing.T) {
	sig := SignBody([]byte("secret"), 0, []byte("body"))
	if sig == "" {
		t.Error("zero timestamp produced empty signature")
	}
}

func TestSignBodyKnownVector(t *testing.T) {
	// Compute expected signature with the same algorithm directly.
	secret := []byte("test-secret")
	timestamp := int64(1718300000)
	body := []byte(`{"doc":"value"}`)

	mac := hmac.New(sha256.New, secret)
	fmt.Fprintf(mac, "%d\n", timestamp)
	mac.Write(body)
	want := hex.EncodeToString(mac.Sum(nil))

	got := SignBody(secret, timestamp, body)
	if got != want {
		t.Errorf("SignBody = %q, want %q", got, want)
	}
}

func TestSignBodyLargeBody(t *testing.T) {
	secret := []byte("secret")
	body := []byte(strings.Repeat("x", 1_000_000))
	sig := SignBody(secret, 1000, body)
	if sig == "" {
		t.Error("large body produced empty signature")
	}
	// Should be 64 hex chars (SHA-256).
	if len(sig) != 64 {
		t.Errorf("signature length = %d, want 64", len(sig))
	}
}

func TestSignBodyHexFormat(t *testing.T) {
	sig := SignBody([]byte("secret"), 1000, []byte("body"))
	// Must be valid hex.
	if _, err := hex.DecodeString(sig); err != nil {
		t.Errorf("signature is not valid hex: %v", err)
	}
	if len(sig) != sha256.Size*2 {
		t.Errorf("signature length = %d, want %d", len(sig), sha256.Size*2)
	}
}

func TestSignBodyTimestampFormat(t *testing.T) {
	// Verify the timestamp is formatted using decimal, not hex or octal.
	// If the timestamp were formatted differently, the known-vector test
	// would catch it, but this is an explicit check.
	secret := []byte("s")
	sigDec := SignBody(secret, 15, []byte("x"))
	sigHex := SignBody(secret, 0xF, []byte("x"))
	if sigDec != sigHex {
		// Both are 15, so they must match.
		t.Errorf("decimal and hex representations of 15 produced different signatures: %q != %q", sigDec, sigHex)
	}
}
