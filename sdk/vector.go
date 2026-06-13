package sdk

import (
	"encoding/json"
	"fmt"
	"maps"

	"github.com/samcharles93/NellDB"
)

// metaVectorID is the synthetic _id the SDK uses to persist its replication
// knowledge vector — the per-peer "highest clock seen" map that drives
// /sync/check and lets the SDK avoid re-fetching records from peers it has
// already heard from.
const metaVectorID = "meta:vector"

// readMetaVector fetches the persisted knowledge vector, if any.
func (d *DocDB) readMetaVector() (nell.KnowledgeVector, bool) {
	rec, err := d.store.Get(d.collection, metaVectorID)
	if err != nil {
		return nil, false
	}
	var kv nell.KnowledgeVector
	if err := json.Unmarshal(rec.Payload, &kv); err != nil {
		return nil, false
	}
	return kv, true
}

// writeMetaVector persists the knowledge vector so a fresh process resumes
// with the same "what have I seen" state.
func (d *DocDB) writeMetaVector(kv nell.KnowledgeVector) error {
	payload, err := json.Marshal(kv)
	if err != nil {
		return fmt.Errorf("sdk: marshal vector: %w", err)
	}
	clk := nell.NewHLC()
	for _, c := range kv {
		clk.Update(c)
	}
	rec := nell.Record{
		Collection: d.collection,
		ID:         metaVectorID,
		Type:       nell.TypeText,
		Payload:    payload,
		Clock:      clk.Tick(),
		UpdatedBy:  d.nodeID,
	}
	if _, _, err := d.store.Put(rec); err != nil {
		return fmt.Errorf("sdk: persist vector: %w", err)
	}
	return nil
}

// observeVector updates the in-memory and persisted knowledge vector with
// the (node, clock) seen in an incoming record.  Called by ingestRemote so
// that subsequent pulls know "I've already seen this from this peer".
func (d *DocDB) observeVector(nodeID string, clock nell.HLC) {
	if nodeID == "" {
		return
	}
	d.mu.Lock()
	if d.vector == nil {
		d.vector = make(nell.KnowledgeVector)
	}
	existing := d.vector[nodeID]
	if clock.GreaterThan(existing) {
		d.vector[nodeID] = clock
		kv := make(nell.KnowledgeVector, len(d.vector))
		maps.Copy(kv, d.vector)
		d.mu.Unlock()
		_ = d.writeMetaVector(kv)
		return
	}
	d.mu.Unlock()
}
