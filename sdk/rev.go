package sdk

import (
	"crypto/sha1"
	"encoding/hex"
	"strconv"
	"strings"
)

// genRev returns an MVCC revision token "<gen>-<sha1>" derived from the
// previous rev and the current document body.  An empty prev produces "1-".
//
// Content-hash revisions let Put detect "I overwrote something I didn't see"
// without a server round-trip — exactly what callers do with the read-modify-
// write pattern.
func genRev(prev string, body []byte) string {
	gen := 1
	if prev != "" {
		parts := strings.SplitN(prev, "-", 2)
		if g, err := strconv.Atoi(parts[0]); err == nil && g > 0 {
			gen = g + 1
		}
	}
	sum := sha1.Sum(body)
	return strconv.Itoa(gen) + "-" + hex.EncodeToString(sum[:])
}

// parseGen extracts the generation number from a rev string.  Returns 0 for
// malformed input.
func parseGen(rev string) int {
	if rev == "" {
		return 0
	}
	g, _ := strconv.Atoi(strings.SplitN(rev, "-", 2)[0])
	return g
}
