package sdk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"time"

	"github.com/samcharles93/NellDB"
)

// Replicator pulls and pushes between this database and a remote NellDB
// HTTP server.  The wire protocol is the existing /sync/pull and /sync/push
// endpoints — no server changes required.
//
// Replication state is persisted via the SDK's meta-clock so a restart
// resumes incremental pulls.  Live mode runs the same pull loop on a timer,
// emitting replicated changes on the SDK's changes feed.
type Replicator struct {
	DB      *DocDB
	BaseURL string
	HTTP    *http.Client
}

// NewReplicator builds a replicator for the given base URL (e.g.
// "https://home.example.com").  The default HTTP client has a 30s timeout;
// pass a custom client to add auth headers, mTLS, etc.
func NewReplicator(db *DocDB, baseURL string) *Replicator {
	return &Replicator{
		DB:      db,
		BaseURL: baseURL,
		HTTP:    &http.Client{Timeout: 30 * time.Second},
	}
}

// Pull fetches every record the server has that we have not yet seen, based
// on the SDK's persisted knowledge vector.  Returns the number ingested.
//
// The wire protocol is /sync/check (per-peer KV diff) rather than /sync/pull
// (single-clock since).  /sync/check handles concurrent writes from new
// peers whose clocks may match ours exactly — a case /sync/pull silently
// drops because its GreaterThan is strict.
func (r *Replicator) Pull(ctx context.Context) (int, error) {
	r.DB.mu.RLock()
	vector := make(nell.KnowledgeVector, len(r.DB.vector))
	maps.Copy(vector, r.DB.vector)
	r.DB.mu.RUnlock()

	body, _ := json.Marshal(map[string]any{
		"sender_node_id": r.DB.nodeID,
		"vector":         vector,
	})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost,
		joinPath(r.BaseURL, "/sync/check"), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.HTTP.Do(req)
	if err != nil {
		return 0, fmt.Errorf("replicate pull: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("replicate pull: %s: %s", resp.Status, raw)
	}

	var out struct {
		MissingChanges []nell.Record `json:"missing_changes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return 0, fmt.Errorf("replicate pull decode: %w", err)
	}
	if out.MissingChanges == nil {
		out.MissingChanges = []nell.Record{}
	}

	var maxSeen nell.HLC
	for _, rec := range out.MissingChanges {
		if err := r.DB.ingestRemote(rec); err != nil {
			return 0, fmt.Errorf("replicate pull ingest %q: %w", rec.ID, err)
		}
		if rec.Clock.GreaterThan(maxSeen) {
			maxSeen = rec.Clock
		}
	}
	if maxSeen.GreaterThan(nell.HLC{}) {
		if err := r.DB.advanceClock(maxSeen); err != nil {
			return len(out.MissingChanges), fmt.Errorf("replicate pull advance clock: %w", err)
		}
	}
	return len(out.MissingChanges), nil
}

// Push sends every local record to the server.  Returns the number the server
// accepted.
func (r *Replicator) Push(ctx context.Context) (int, error) {
	all, err := r.DB.store.List()
	if err != nil {
		return 0, fmt.Errorf("replicate push list: %w", err)
	}
	// Filter out SDK-internal bookkeeping records (meta:clock,
	// meta:vector).  They're local state and would otherwise be
	// re-delivered by /sync/check, since they have UpdatedBy=local and a
	// newer clock than the user's records.
	filtered := all[:0]
	for _, rec := range all {
		if isInternalID(rec.ID) {
			continue
		}
		filtered = append(filtered, rec)
	}

	body, _ := json.Marshal(map[string][]nell.Record{"changes": filtered})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost,
		joinPath(r.BaseURL, "/sync/push"), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.HTTP.Do(req)
	if err != nil {
		return 0, fmt.Errorf("replicate push: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("replicate push: %s: %s", resp.Status, raw)
	}

	var out struct {
		Accepted int `json:"accepted"`
		Total    int `json:"total"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return 0, fmt.Errorf("replicate push decode: %w", err)
	}
	// After pushing, the server has seen our local records — the
	// last-seen-clock should at least cover everything we sent.
	var maxSent nell.HLC
	for _, rec := range filtered {
		if rec.Clock.GreaterThan(maxSent) {
			maxSent = rec.Clock
		}
	}
	if maxSent.GreaterThan(nell.HLC{}) {
		_ = r.DB.advanceClock(maxSent)
	}
	return out.Accepted, nil
}

// Sync runs Push then Pull and returns (pushed, pulled, err).  One
// round-trip with a peer — equivalent to "replicate to + replicate from".
func (r *Replicator) Sync(ctx context.Context) (pushed, pulled int, err error) {
	pushed, err = r.Push(ctx)
	if err != nil {
		return 0, 0, err
	}
	pulled, err = r.Pull(ctx)
	return pushed, pulled, err
}

// LiveConfig configures a Live replication loop.
type LiveConfig struct {
	Interval   time.Duration // pull cadence (default 5s)
	PushEvery  int           // push every N pulls (default 1 = every pull cycle)
	BackoffMax time.Duration // cap on backoff between failed pulls (default 1m)
}

// Live starts a background sync loop and returns a stop function.  The loop
// pushes and pulls at the configured interval, with exponential backoff on
// errors.  Replicated changes are broadcast on db.Changes() as if they were
// local writes.
func (r *Replicator) Live(ctx context.Context, cfg LiveConfig) (stop func()) {
	if cfg.Interval <= 0 {
		cfg.Interval = 5 * time.Second
	}
	if cfg.PushEvery <= 0 {
		cfg.PushEvery = 1
	}
	if cfg.BackoffMax <= 0 {
		cfg.BackoffMax = time.Minute
	}

	loopCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	backoff := cfg.Interval

	go func() {
		defer close(done)
		pullCount := 0
		t := time.NewTimer(cfg.Interval)
		defer t.Stop()
		for {
			select {
			case <-loopCtx.Done():
				return
			case <-t.C:
				if pullCount%cfg.PushEvery == 0 {
					if _, err := r.Push(loopCtx); err != nil {
						backoff = nextBackoff(backoff, cfg.BackoffMax)
						t.Reset(backoff)
						continue
					}
				}
				if _, err := r.Pull(loopCtx); err != nil {
					backoff = nextBackoff(backoff, cfg.BackoffMax)
					t.Reset(backoff)
					continue
				}
				pullCount++
				backoff = cfg.Interval
				t.Reset(backoff)
			}
		}
	}()

	return func() {
		cancel()
		<-done
	}
}

// nextBackoff doubles the delay up to max.  A "real" implementation would
// also reset on success, which the Live loop does by reassigning backoff.
func nextBackoff(cur, max time.Duration) time.Duration {
	next := cur * 2
	if next > max {
		return max
	}
	return next
}

// ── internal ─────────────────────────────────────────────────────────────────

// ingestRemote applies a record received from a peer to the local store.
// The SDK reads the rev from the payload (where the wire format carries it)
// and refreshes its in-memory cache; the engine's LWW decides whether the
// local or remote clock wins.  The (UpdatedBy, Clock) pair is folded into
// the knowledge vector so subsequent pulls know we have seen this record.
//
// Internal meta:* records are silently dropped — they're local bookkeeping
// and should not have been on the wire in the first place.  Belt-and-braces:
// even if a peer sends one (or a previous version of this SDK did), we
// never ingest it.
func (d *DocDB) ingestRemote(rec nell.Record) error {
	if isInternalID(rec.ID) {
		return nil
	}
	if _, _, err := d.store.Put(rec); err != nil {
		return err
	}
	rev, ok := readRev(rec)
	if !ok {
		rev = "1-remote"
	}
	d.mu.Lock()
	d.revs[rec.ID] = rev
	d.mu.Unlock()
	d.observeVector(rec.UpdatedBy, rec.Clock)

	doc := joinDoc(rec.ID, rec.Payload)
	if rec.Deleted {
		doc[FieldDeleted] = true
	}
	d.subs.broadcast(Change{ID: rec.ID, Rev: rev, Deleted: rec.Deleted, Doc: doc})
	return nil
}

func joinPath(base, path string) string {
	u, err := url.Parse(base)
	if err != nil {
		return base + path
	}
	u.Path = u.Path + path
	return u.String()
}

// isInternalID reports whether an id is one of the SDK's own bookkeeping
// records (meta:clock, meta:vector) that should not be replicated.
func isInternalID(id string) bool {
	return len(id) >= 5 && id[:5] == "meta:"
}
