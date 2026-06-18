package server

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/samcharles93/NellDB"
)

func TestHMACAuthEmptySecretPassthrough(t *testing.T) {
	mw := HMACAuth(nil)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))

	req := httptest.NewRequest(http.MethodPost, "/sync/push", strings.NewReader(`{"test":true}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("empty secret: expected 200, got %d", rec.Code)
	}
}

func TestHMACAuthEmptySlicePassthrough(t *testing.T) {
	mw := HMACAuth([]byte{})
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/sync/push", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("empty slice secret: expected 200, got %d", rec.Code)
	}
}

func TestHMACAuthMissingTimestamp(t *testing.T) {
	mw := HMACAuth([]byte("secret"))
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	}))

	req := httptest.NewRequest(http.MethodPost, "/sync/push", strings.NewReader(`{}`))
	req.Header.Set("X-Nell-Signature", "abcd")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestHMACAuthMissingSignature(t *testing.T) {
	mw := HMACAuth([]byte("secret"))
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	}))

	req := httptest.NewRequest(http.MethodPost, "/sync/push", strings.NewReader(`{}`))
	req.Header.Set("X-Nell-Timestamp", "1000")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestHMACAuthInvalidTimestamp(t *testing.T) {
	mw := HMACAuth([]byte("secret"))
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	}))

	req := httptest.NewRequest(http.MethodPost, "/sync/push", strings.NewReader(`{}`))
	req.Header.Set("X-Nell-Timestamp", "not-a-number")
	req.Header.Set("X-Nell-Signature", "abcd")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for invalid timestamp, got %d", rec.Code)
	}
}

func TestHMACAuthTimestampTooOld(t *testing.T) {
	mw := HMACAuth([]byte("secret"))
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	}))

	old := time.Now().Add(-DefaultMaxSkew - time.Second).Unix()
	sig := nell.SignBody([]byte("secret"), old, []byte(`{}`))

	req := httptest.NewRequest(http.MethodPost, "/sync/push", strings.NewReader(`{}`))
	req.Header.Set("X-Nell-Timestamp", fmt.Sprintf("%d", old))
	req.Header.Set("X-Nell-Signature", sig)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for too-old timestamp, got %d", rec.Code)
	}
}

func TestHMACAuthTimestampTooFuture(t *testing.T) {
	mw := HMACAuth([]byte("secret"))
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	}))

	future := time.Now().Add(DefaultMaxSkew + time.Second).Unix()
	sig := nell.SignBody([]byte("secret"), future, []byte(`{}`))

	req := httptest.NewRequest(http.MethodPost, "/sync/push", strings.NewReader(`{}`))
	req.Header.Set("X-Nell-Timestamp", fmt.Sprintf("%d", future))
	req.Header.Set("X-Nell-Signature", sig)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for future timestamp, got %d", rec.Code)
	}
}

func TestHMACAuthBadSignature(t *testing.T) {
	mw := HMACAuth([]byte("secret"))
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	}))

	ts := time.Now().Unix()
	req := httptest.NewRequest(http.MethodPost, "/sync/push", strings.NewReader(`{}`))
	req.Header.Set("X-Nell-Timestamp", fmt.Sprintf("%d", ts))
	req.Header.Set("X-Nell-Signature", "badbadbadbadbadbadbadbadbadbadbadbadbadbadbadbad")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for bad signature, got %d", rec.Code)
	}
}

func TestHMACAuthWrongSecret(t *testing.T) {
	mw := HMACAuth([]byte("real-secret"))
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	}))

	ts := time.Now().Unix()
	// Sign with a different secret.
	sig := nell.SignBody([]byte("wrong-secret"), ts, []byte(`{}`))

	req := httptest.NewRequest(http.MethodPost, "/sync/push", strings.NewReader(`{}`))
	req.Header.Set("X-Nell-Timestamp", fmt.Sprintf("%d", ts))
	req.Header.Set("X-Nell-Signature", sig)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for wrong secret, got %d", rec.Code)
	}
}

func TestHMACAuthValidRequest(t *testing.T) {
	secret := []byte("my-secret")
	body := []byte(`{"doc":"test"}`)

	mw := HMACAuth(secret)
	var receivedBody []byte
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var err error
		receivedBody, err = io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("downstream handler failed to read body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))

	ts := time.Now().Unix()
	sig := nell.SignBody(secret, ts, body)

	req := httptest.NewRequest(http.MethodPost, "/sync/push", bytes.NewReader(body))
	req.Header.Set("X-Nell-Timestamp", fmt.Sprintf("%d", ts))
	req.Header.Set("X-Nell-Signature", sig)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !bytes.Equal(receivedBody, body) {
		t.Errorf("downstream received body %q, want %q", receivedBody, body)
	}
}

func TestHMACAuthBodyPreservedForDownstream(t *testing.T) {
	secret := []byte("secret")
	body := []byte(strings.Repeat("payload-data,", 100))

	mw := HMACAuth(secret)
	var received []byte
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var err error
		received, err = io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("downstream read: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))

	ts := time.Now().Unix()
	sig := nell.SignBody(secret, ts, body)

	req := httptest.NewRequest(http.MethodPost, "/sync/push", bytes.NewReader(body))
	req.Header.Set("X-Nell-Timestamp", fmt.Sprintf("%d", ts))
	req.Header.Set("X-Nell-Signature", sig)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !bytes.Equal(received, body) {
		t.Errorf("body mismatch: len=%d vs len=%d", len(received), len(body))
	}
}

func TestHMACAuthValidAtSkewBoundary(t *testing.T) {
	secret := []byte("secret")

	mw := HMACAuth(secret)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Use half the skew window to avoid timing race between test and server clocks.
	ts := time.Now().Add(-DefaultMaxSkew / 2).Unix()
	sig := nell.SignBody(secret, ts, []byte(`{}`))

	req := httptest.NewRequest(http.MethodPost, "/sync/push", strings.NewReader(`{}`))
	req.Header.Set("X-Nell-Timestamp", fmt.Sprintf("%d", ts))
	req.Header.Set("X-Nell-Signature", sig)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("timestamp within window: expected 200, got %d", rec.Code)
	}
}

func TestHMACAuthGETPassthrough(t *testing.T) {
	// GET requests should also go through auth (the middleware wraps all methods).
	secret := []byte("secret")

	mw := HMACAuth(secret)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	ts := time.Now().Unix()
	sig := nell.SignBody(secret, ts, nil)

	req := httptest.NewRequest(http.MethodGet, "/sync/health", nil)
	req.Header.Set("X-Nell-Timestamp", fmt.Sprintf("%d", ts))
	req.Header.Set("X-Nell-Signature", sig)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("GET with valid auth: expected 200, got %d", rec.Code)
	}
}

func TestBodyReaderRead(t *testing.T) {
	data := []byte("hello world")
	r := &bodyReader{data: data}

	// Read in chunks smaller than the data.
	buf := make([]byte, 5)
	var result []byte
	for {
		n, err := r.Read(buf)
		result = append(result, buf[:n]...)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if !bytes.Equal(result, data) {
		t.Errorf("bodyReader: got %q, want %q", result, data)
	}
}

func TestBodyReaderReadEmpty(t *testing.T) {
	r := &bodyReader{data: []byte{}}
	buf := make([]byte, 10)
	n, err := r.Read(buf)
	if n != 0 {
		t.Errorf("empty read: got %d bytes, want 0", n)
	}
	if err != io.EOF {
		t.Errorf("empty read: got error %v, want io.EOF", err)
	}
}

func TestBodyReaderReadExact(t *testing.T) {
	data := []byte("exact")
	r := &bodyReader{data: data}
	buf := make([]byte, len(data))
	n, err := r.Read(buf)
	if err != nil && err != io.EOF {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != len(data) {
		t.Errorf("read %d bytes, want %d", n, len(data))
	}
	if !bytes.Equal(buf[:n], data) {
		t.Errorf("got %q, want %q", buf[:n], data)
	}

	// Second read should return EOF.
	n, err = r.Read(buf)
	if n != 0 || err != io.EOF {
		t.Errorf("second read: n=%d, err=%v, want 0, io.EOF", n, err)
	}
}

func TestHMACAuthLongTimestamp(t *testing.T) {
	// strconv.ParseInt handles int64 range.
	mw := HMACAuth([]byte("secret"))
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called for timestamp 0")
	}))

	// Timestamp 0 is far in the past, should be rejected on skew.
	req := httptest.NewRequest(http.MethodPost, "/sync/push", strings.NewReader(`{}`))
	req.Header.Set("X-Nell-Timestamp", "0")
	req.Header.Set("X-Nell-Signature", "abcd")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("timestamp 0: expected 401, got %d", rec.Code)
	}
}

func TestServerSignBodyDeprecated(t *testing.T) {
	// Verify the deprecated wrapper produces the same result.
	secret := []byte("s")
	ts := int64(1000)
	body := []byte("b")

	got := SignBody(secret, ts, body)
	want := nell.SignBody(secret, ts, body)
	if got != want {
		t.Errorf("deprecated SignBody: %q != nell.SignBody: %q", got, want)
	}
}

func TestHMACAuthTamperedBody(t *testing.T) {
	// Client signs body A, but sends body B — should fail.
	secret := []byte("secret")
	bodySigned := []byte(`{"good":"data"}`)
	bodySent := []byte(`{"evil":"data"}`)

	mw := HMACAuth(secret)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	}))

	ts := time.Now().Unix()
	sig := nell.SignBody(secret, ts, bodySigned)

	req := httptest.NewRequest(http.MethodPost, "/sync/push", bytes.NewReader(bodySent))
	req.Header.Set("X-Nell-Timestamp", fmt.Sprintf("%d", ts))
	req.Header.Set("X-Nell-Signature", sig)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("tampered body: expected 401, got %d", rec.Code)
	}
}

func TestHMACAuthNoHeadersAtAll(t *testing.T) {
	mw := HMACAuth([]byte("secret"))
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	}))

	req := httptest.NewRequest(http.MethodPost, "/sync/push", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("no headers: expected 401, got %d", rec.Code)
	}
}

// preventImportStripping ensures crypto/hmac and crypto/sha256 are kept when
// we only use them indirectly via nell.SignBody.
var _ = hmac.Equal
var _ = sha256.New
