package sdk

import (
	"encoding/json"
	"fmt"

	"github.com/samcharles93/nell-engine"
)

// metaClockID is the synthetic _id the SDK uses to persist its last-seen
// replication clock.  It rides inside the regular nell.Record store, hidden
// from the user's AllDocs via the StartKey/EndKey range they pick.
const metaClockID = "meta:clock"

// readMetaClock fetches the persisted last-seen clock, if any.
func (d *DocDB) readMetaClock() (nell.HLC, bool) {
	rec, err := d.store.Get(metaClockID)
	if err != nil {
		return nell.HLC{}, false
	}
	var c nell.HLC
	if err := json.Unmarshal(rec.Payload, &c); err != nil {
		return nell.HLC{}, false
	}
	return c, true
}

// writeMetaClock stores the latest clock the database has ever seen.  Called
// after every successful replication round.
func (d *DocDB) writeMetaClock(c nell.HLC) error {
	payload, err := json.Marshal(c)
	if err != nil {
		return fmt.Errorf("sdk: marshal clock: %w", err)
	}
	// Use a clock greater than c so the LWW dance doesn't reject the meta
	// record as a stale overwrite when the same clock appears repeatedly.
	clk := nell.NewHLC()
	clk.Update(c)
	rec := nell.Record{
		ID:        metaClockID,
		Type:      nell.TypeText,
		Payload:   payload,
		Clock:     clk.Tick(),
		UpdatedBy: d.nodeID,
	}
	if _, _, err := d.store.Put(rec); err != nil {
		return fmt.Errorf("sdk: persist clock: %w", err)
	}
	return nil
}

// advanceClock updates the in-memory and persisted last-seen clock.  The
// persisted form is the only thing that lets a fresh process resume
// incremental pulls; without it, a restart would re-fetch the entire
// database.
func (d *DocDB) advanceClock(c nell.HLC) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !c.GreaterThan(d.lastSeenClock) {
		return nil
	}
	d.lastSeenClock = c
	return d.writeMetaClock(c)
}
