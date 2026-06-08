// Package server — HMAC request signing for sync endpoints.
//
// Scheme:
//
//	Client computes:  signature = HMAC-SHA256(secret, timestamp + "\n" + body_bytes)
//	Sends headers:    X-Nell-Timestamp: <unix_seconds>
//	                  X-Nell-Signature: <hex>
//
//	Server validates: timestamp within ±maxSkew, recomputes HMAC, compares constant-time.
package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"
)

// DefaultMaxSkew is the allowed clock drift for HMAC timestamps.
const DefaultMaxSkew = 30 * time.Second

// HMACAuth returns middleware that requires valid HMAC signatures on every
// request.  When secret is empty, requests pass through unauthenticated.
func HMACAuth(secret []byte) func(http.Handler) http.Handler {
	if len(secret) == 0 {
		return func(next http.Handler) http.Handler { return next }
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tsStr := r.Header.Get("X-Nell-Timestamp")
			sigStr := r.Header.Get("X-Nell-Signature")
			if tsStr == "" || sigStr == "" {
				writeJSON(w, http.StatusUnauthorized, map[string]any{
					"error": "missing authentication headers",
				})
				return
			}

			ts, err := strconv.ParseInt(tsStr, 10, 64)
			if err != nil {
				writeJSON(w, http.StatusUnauthorized, map[string]any{
					"error": "invalid timestamp",
				})
				return
			}

			skew := time.Since(time.Unix(ts, 0))
			if skew < -DefaultMaxSkew || skew > DefaultMaxSkew {
				writeJSON(w, http.StatusUnauthorized, map[string]any{
					"error": "timestamp out of range",
				})
				return
			}

			// Read the body to validate signature, then put it back
			// so downstream handlers can decode it.
			body, err := io.ReadAll(r.Body)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]any{
					"error": "failed to read body",
				})
				return
			}
			_ = r.Body.Close()

			want := signBody(secret, ts, body)
			if !hmac.Equal([]byte(want), []byte(sigStr)) {
				log.Printf("[auth] bad signature from %s for %s %s", r.RemoteAddr, r.Method, r.URL.Path)
				writeJSON(w, http.StatusUnauthorized, map[string]any{
					"error": "invalid signature",
				})
				return
			}

			// Reconstruct body for the handler.
			r.Body = io.NopCloser(&bodyReader{data: body})

			next.ServeHTTP(w, r)
		})
	}
}

// bodyReader is an io.Reader backed by a byte slice.
type bodyReader struct {
	data []byte
	pos  int
}

func (r *bodyReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n := copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}

// SignBody computes the HMAC-SHA256 of (timestamp + "\n" + body), hex-encoded.
func SignBody(secret []byte, timestamp int64, body []byte) string {
	return signBody(secret, timestamp, body)
}

func signBody(secret []byte, timestamp int64, body []byte) string {
	mac := hmac.New(sha256.New, secret)
	_, _ = fmt.Fprintf(mac, "%d\n", timestamp)
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}
