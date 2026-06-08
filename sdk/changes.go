package sdk

import (
	"sync"
)

// changesHub is a fan-out for the local changes feed.  Subscribers receive
// every change broadcast via DocDB.Put / Remove / ingestRemote.  Slow
// subscribers do not block the writer — the channel buffer (16) drops
// changes when full; callers that need a complete feed should also use
// AllDocs on reconnect to catch up.
type changesHub struct {
	mu     sync.Mutex
	subs   map[uint64]chan Change
	nextID uint64
}

func newChangesHub() *changesHub {
	return &changesHub{subs: make(map[uint64]chan Change)}
}

// subscribe returns a channel that receives local changes and an unsubscribe
// function.  The channel is closed when the caller invokes the returned
// cancel func.
func (h *changesHub) subscribe() (uint64, chan Change, func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	id := h.nextID
	h.nextID++
	ch := make(chan Change, 16)
	h.subs[id] = ch
	return id, ch, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if c, ok := h.subs[id]; ok {
			delete(h.subs, id)
			close(c)
		}
	}
}

func (h *changesHub) broadcast(c Change) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, ch := range h.subs {
		select {
		case ch <- c:
		default:
			// Drop on full subscriber.
		}
	}
}
