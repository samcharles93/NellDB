package sdk

import "context"

// Changes returns a channel of local changes.  The channel is closed when ctx
// is cancelled AND the hub's pending changes have been drained.
//
// Drain semantics: when ctx fires, the goroutine enters a non-blocking drain
// loop that forwards every remaining change from the hub's internal channel
// to the outbound channel.  This makes Changes safe to cancel at any time
// without losing events the writer has already published.
//
// Backpressure: the outbound channel is buffered to 64.  Slower consumers
// may drop changes rather than block the writer — callers that need a
// complete feed should also call AllDocs on reconnect to catch up.
func (d *DocDB) Changes(ctx context.Context) <-chan Change {
	out := make(chan Change, 64)
	_, src, unsub := d.subs.subscribe()
	go func() {
		defer unsub()
		defer close(out)

		// forward writes c to out.  It tries non-blocking first so that
		// when both `out <- c` and `ctx.Done()` are ready (consumer slow
		// + cancel fired) we never race the select into dropping the
		// change.  Only when out is actually full do we block, and then
		// ctx is the safety valve.
		forward := func(c Change) {
			select {
			case out <- c:
				return
			default:
			}
			select {
			case out <- c:
			case <-ctx.Done():
			}
		}

		for {
			select {
			case <-ctx.Done():
				for {
					select {
					case c, ok := <-src:
						if !ok {
							return
						}
						forward(c)
					default:
						return
					}
				}
			case c, ok := <-src:
				if !ok {
					return
				}
				forward(c)
			}
		}
	}()
	return out
}
