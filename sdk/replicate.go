package sdk

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"math/rand"
	"net/http"
	"net/url"
	"time"

	"github.com/gorilla/websocket"
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
	DB         *DocDB
	BaseURL    string
	HTTP       *http.Client
	authSecret []byte
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

// SetAuthSecret configures the Replicator to HMAC-sign all HTTP requests
// with the given secret.  The server must be configured with the same secret
// for the requests to be accepted.  Pass nil to disable signing.
func (r *Replicator) SetAuthSecret(secret []byte) {
	r.authSecret = secret
	if len(secret) == 0 {
		r.HTTP.Transport = nil
		return
	}
	r.HTTP.Transport = &signingTransport{
		secret:    secret,
		transport: http.DefaultTransport,
	}
}

// Pull fetches every record the server has that we have not yet seen,
// using the high-performance binary protocol.
func (r *Replicator) Pull(ctx context.Context) (int, error) {
	r.DB.mu.RLock()
	vector := make(nell.KnowledgeVector, len(r.DB.vector))
	maps.Copy(vector, r.DB.vector)
	r.DB.mu.RUnlock()

	body, err := vector.MarshalBinary()
	if err != nil {
		return 0, fmt.Errorf("replicate pull marshal: %w", err)
	}

	u, _ := url.Parse(joinPath(r.BaseURL, "/sync/bin/check"))
	q := u.Query()
	q.Set("col", r.DB.collection)
	u.RawQuery = q.Encode()

	req, _ := http.NewRequestWithContext(ctx, http.MethodPost,
		u.String(), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := r.HTTP.Do(req)
	if err != nil {
		return 0, fmt.Errorf("replicate pull request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("replicate pull server error (%d): %s",
			resp.StatusCode, string(msg))
	}

	ingested := 0
	var maxSeen nell.HLC
	for {
		var header [4]byte
		_, err := io.ReadFull(resp.Body, header[:])
		if err == io.EOF {
			break
		}
		if err != nil {
			return ingested, fmt.Errorf("replicate pull read frame header: %w", err)
		}

		recLen := binary.BigEndian.Uint32(header[:])
		recBytes := make([]byte, recLen)
		if _, err := io.ReadFull(resp.Body, recBytes); err != nil {
			return ingested, fmt.Errorf("replicate pull read frame data: %w", err)
		}

		var rec nell.Record
		if err := rec.UnmarshalBinary(recBytes); err != nil {
			return ingested, fmt.Errorf("replicate pull unmarshal record: %w", err)
		}

		if err := r.DB.ingestRemote(rec); err != nil {
			return ingested, fmt.Errorf("replicate pull ingest %q: %w", rec.ID, err)
		}
		if rec.Clock.GreaterThan(maxSeen) {
			maxSeen = rec.Clock
		}
		ingested++
	}

	if maxSeen.GreaterThan(nell.HLC{}) {
		if err := r.DB.advanceClock(maxSeen); err != nil {
			return ingested, fmt.Errorf("replicate pull advance clock: %w", err)
		}
	}

	return ingested, nil
}

// Push uploads all locally-held records the local node has in the
// current collection, including tombstones, using the high-performance
// binary protocol so peers learn about deletions.
func (r *Replicator) Push(ctx context.Context) (int, error) {
	all, err := r.DB.store.ListAll(r.DB.collection)
	if err != nil {
		return 0, fmt.Errorf("replicate push list: %w", err)
	}

	filtered := all[:0]
	for _, rec := range all {
		if isInternalID(rec.ID) {
			continue
		}
		filtered = append(filtered, rec)
	}

	// We stream the binary frames into a buffer for now.
	var buf bytes.Buffer
	for _, rec := range filtered {
		recBytes, err := rec.MarshalBinary()
		if err != nil {
			continue
		}
		var header [4]byte
		binary.BigEndian.PutUint32(header[:], uint32(len(recBytes)))
		buf.Write(header[:])
		buf.Write(recBytes)
	}

	u, _ := url.Parse(joinPath(r.BaseURL, "/sync/bin/push"))
	q := u.Query()
	q.Set("col", r.DB.collection)
	u.RawQuery = q.Encode()

	req, _ := http.NewRequestWithContext(ctx, http.MethodPost,
		u.String(), &buf)
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := r.HTTP.Do(req)
	if err != nil {
		return 0, fmt.Errorf("replicate push request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("replicate push server error (%d): %s",
			resp.StatusCode, string(msg))
	}

	var respHeader [4]byte
	if _, err := io.ReadFull(resp.Body, respHeader[:]); err != nil {
		return 0, fmt.Errorf("replicate push read response: %w", err)
	}
	accepted := int(binary.BigEndian.Uint32(respHeader[:]))

	// After pushing, the server has seen our local records.
	var maxSent nell.HLC
	for _, rec := range filtered {
		if rec.Clock.GreaterThan(maxSent) {
			maxSent = rec.Clock
		}
	}
	if maxSent.GreaterThan(nell.HLC{}) {
		_ = r.DB.advanceClock(maxSent)
	}

	return accepted, nil
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

// LiveWS starts a background sync loop over a persistent WebSocket connection.
// Unlike Live (HTTP polling), LiveWS receives changes in real time with lower
// latency.  Returns a stop function; call it to shut down the connection.
//
// On disconnect the client reconnects with exponential backoff and jitter.
func (r *Replicator) LiveWS(ctx context.Context, nodeID string) (stop func()) {
	loopCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	backoff := 2 * time.Second
	const maxBackoff = 60 * time.Second

	go func() {
		defer close(done)
		for {
			select {
			case <-loopCtx.Done():
				return
			default:
			}

			if err := r.runWS(loopCtx, nodeID); err != nil {
				// Only log if not a clean shutdown.
				select {
				case <-loopCtx.Done():
					return
				default:
				}
			} else {
				backoff = 2 * time.Second
			}

			// Reconnect with backoff + jitter.
			jitter := time.Duration(rand.Int63n(int64(backoff / 2)))
			select {
			case <-loopCtx.Done():
				return
			case <-time.After(backoff + jitter):
			}
			backoff = nextBackoff(backoff, maxBackoff)
		}
	}()

	return func() {
		cancel()
		<-done
	}
}

// runWS maintains a single WebSocket session: connects, reads incoming
// changes, and writes local changes.  Returns on disconnect or error.
func (r *Replicator) runWS(ctx context.Context, nodeID string) error {
	// Build WebSocket URL from base URL.
	wsURL := r.BaseURL + "/sync/ws"
	if nodeID != "" {
		wsURL += "?node_id=" + url.QueryEscape(nodeID)
	}
	// Convert http:// → ws://, https:// → wss://
	if len(wsURL) > 7 && wsURL[:7] == "http://" {
		wsURL = "ws" + wsURL[4:]
	} else if len(wsURL) > 8 && wsURL[:8] == "https://" {
		wsURL = "wss" + wsURL[5:]
	}

	reqHeader := http.Header{}
	if r.authSecret != nil {
		ts := time.Now().Unix()
		sig := nell.SignBody(r.authSecret, ts, nil)
		reqHeader.Set("X-Nell-Timestamp", fmt.Sprintf("%d", ts))
		reqHeader.Set("X-Nell-Signature", sig)
	}
	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}
	conn, _, err := dialer.DialContext(ctx, wsURL, reqHeader)
	if err != nil {
		return err
	}
	defer conn.Close()

	// Subscribe to local changes for push to the server.
	changes := r.DB.Changes(ctx)

	readDone := make(chan struct{})

	// Read goroutine: receive remote changes from server.
	go func() {
		defer close(readDone)
		for {
			conn.SetReadDeadline(time.Now().Add(5 * time.Second))
			var msg struct {
				Changes []nell.Record `json:"changes"`
			}
			if err := conn.ReadJSON(&msg); err != nil {
				return
			}
			for _, rec := range msg.Changes {
				if err := r.DB.ingestRemote(rec); err != nil {
					slog.Warn("livews: ingest remote failed", "id", rec.ID, "err", err)
				}
			}
		}
	}()

	// Write goroutine: send local changes to server.
	writeDone := make(chan struct{})
	go func() {
		defer close(writeDone)
		for {
			select {
			case <-ctx.Done():
				return
			case <-readDone:
				return
			case ch, ok := <-changes:
				if !ok {
					return
				}
				if isInternalID(ch.ID) {
					continue
				}
				// Build a record from the store for the wire.
				rec, err := r.DB.store.Get(r.DB.collection, ch.ID)
				if err != nil {
					if ch.Doc != nil {
						payload, _ := json.Marshal(ch.Doc)
						rec = nell.Record{
							ID:      ch.ID,
							Deleted: ch.Deleted,
							Payload: payload,
						}
					} else {
						slog.Warn("livews: dropped local change, store.Get failed", "id", ch.ID, "err", err)
						continue
					}
				}
				msg := map[string]any{
					"changes": []nell.Record{rec},
				}
				if err := conn.WriteJSON(msg); err != nil {
					return
				}
			}
		}
	}()

	// Wait for either goroutine to exit.
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-readDone:
		<-writeDone
	case <-writeDone:
		conn.Close()
		<-readDone
	}
	return nil
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

// signingTransport is an http.RoundTripper that adds HMAC signature headers
// to every request.  It uses the shared nell.SignBody function so the server
// can validate signatures with the same secret.
type signingTransport struct {
	secret    []byte
	transport http.RoundTripper
}

func (t *signingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Read and sign the body.
	var bodyBytes []byte
	if req.Body != nil {
		var err error
		bodyBytes, err = io.ReadAll(req.Body)
		req.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("read body for signing: %w", err)
		}
	}
	ts := time.Now().Unix()
	sig := nell.SignBody(t.secret, ts, bodyBytes)

	// Reconstruct body.
	if bodyBytes != nil {
		req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
	}
	req.ContentLength = int64(len(bodyBytes))

	req.Header.Set("X-Nell-Timestamp", fmt.Sprintf("%d", ts))
	req.Header.Set("X-Nell-Signature", sig)

	if t.transport != nil {
		return t.transport.RoundTrip(req)
	}
	return http.DefaultTransport.RoundTrip(req)
}

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

	doc := joinDoc(rec.ID, rec)
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
