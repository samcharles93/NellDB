//go:build js && wasm

package nell

import (
	"encoding/hex"
	"fmt"
	"syscall/js"
)

// GenerateUUIDv4 returns a fresh RFC 4122 version 4 UUID string.  Uses
// crypto.getRandomValues from the JS global to seed 16 random bytes,
// then sets the version (4) and variant (RFC 4122) bits per the UUID spec.
//
// Returns an error if the runtime does not expose crypto.  In Node and all
// modern browsers this should not happen.
func GenerateUUIDv4() (string, error) {
	crypto := js.Global().Get("crypto")
	if crypto.IsUndefined() {
		return "", fmt.Errorf("nell: crypto is not available in this runtime")
	}
	// crypto.getRandomValues must be invoked as a method on crypto so the
	// `this` binding is the Crypto object.  Calling Get("getRandomValues")
	// and then Invoke loses the receiver, which Node/Webkit reject with
	// "Value of 'this' must be of type Crypto".
	if crypto.Get("getRandomValues").IsUndefined() {
		return "", fmt.Errorf("nell: crypto.getRandomValues is not available")
	}

	// 16 random bytes, copied out of the JS Uint8Array view.
	buf := make([]byte, 16)
	view := js.Global().Get("Uint8Array").New(len(buf))
	crypto.Call("getRandomValues", view)
	js.CopyBytesToGo(buf, view)

	// RFC 4122: version 4 (random) and variant 10
	buf[6] = (buf[6] & 0x0F) | 0x40
	buf[8] = (buf[8] & 0x3F) | 0x80

	hexStr := hex.EncodeToString(buf)
	return hexStr[0:8] + "-" + hexStr[8:12] + "-" + hexStr[12:16] + "-" + hexStr[16:20] + "-" + hexStr[20:32], nil
}
