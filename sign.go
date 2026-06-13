// Package nell — shared HMAC request signing used by both server and SDK.
//
// Scheme:
//
//	Client computes:  signature = HMAC-SHA256(secret, timestamp + "\n" + body_bytes)
//	Sends headers:    X-Nell-Timestamp: <unix_seconds>
//	                  X-Nell-Signature: <hex>
//
//	Server validates: timestamp within ±maxSkew, recomputes HMAC, compares constant-time.
package nell

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// SignBody computes the HMAC-SHA256 of (timestamp + "\n" + body), hex-encoded.
func SignBody(secret []byte, timestamp int64, body []byte) string {
	mac := hmac.New(sha256.New, secret)
	_, _ = fmt.Fprintf(mac, "%d\n", timestamp)
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}
