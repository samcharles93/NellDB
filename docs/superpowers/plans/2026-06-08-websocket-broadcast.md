# WebSocket Broadcast Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement real-time WebSocket push between mesh peers and connected clients to remove the `TODO` markers.

**Architecture:** We will use `github.com/gorilla/websocket` to add a `/sync/ws` endpoint to the server. The `Server` struct will track active connections (`peerConn`). The `broadcast` method will serialize and push mutations to all connected WebSockets. `MeshManager` will route local mutations through this broadcast method.

**Tech Stack:** Go, `github.com/gorilla/websocket`

---

### Task 1: Add WebSocket endpoint to `server/main.go`

**Files:**
- Modify: `server/main.go`

- [ ] **Step 1: Get the dependency**

Run: `go get github.com/gorilla/websocket`

- [ ] **Step 2: Update `peerConn` and `Server`**

```go
// Replace the peerConn struct in server/main.go
import "github.com/gorilla/websocket"

type peerConn struct {
	nodeID string
	conn   *websocket.Conn
	mu     sync.Mutex // protects concurrent writes to the websocket
}
```

Add an `upgrader` variable at the package level:

```go
var upgrader = websocket.Upgrader{
	// Accept all origins for now to avoid CORS issues for WASM clients
	CheckOrigin: func(r *http.Request) bool { return true },
}
```

- [ ] **Step 3: Implement `handleWebSocket`**

Add this method to `server/main.go`:

```go
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		logError("handleWebSocket", "upgrade failed", err)
		return
	}

	nodeID := r.URL.Query().Get("node_id")
	if nodeID == "" {
		nodeID = "unknown"
	}

	pConn := &peerConn{
		nodeID: nodeID,
		conn:   conn,
	}

	s.mu.Lock()
	s.peers[conn.RemoteAddr().String()] = pConn
	s.mu.Unlock()

	slog.Info("websocket connected", "peer", nodeID, "addr", conn.RemoteAddr().String())

	// Read loop to detect disconnects and process any incoming push messages
	go func() {
		defer func() {
			s.mu.Lock()
			delete(s.peers, conn.RemoteAddr().String())
			s.mu.Unlock()
			conn.Close()
			slog.Info("websocket disconnected", "peer", nodeID, "addr", conn.RemoteAddr().String())
		}()

		for {
			var req struct {
				Changes []nell.Record `json:"changes"`
			}
			err := conn.ReadJSON(&req)
			if err != nil {
				break
			}
			// Process incoming changes directly like handlePush
			var accepted int
			for _, rec := range req.Changes {
				ok, _, err := s.store.Put(rec)
				if err != nil {
					continue
				}
				if ok {
					accepted++
				}
				s.recordSeen(rec)
			}
			if accepted > 0 {
				s.broadcast(req.Changes)
			}
		}
	}()
}
```

- [ ] **Step 4: Register the endpoint**

In `func (s *Server) Handler() http.Handler`, add:
```go
	mux.HandleFunc("/sync/ws", s.handleWebSocket)
```

### Task 2: Implement the `broadcast` fan-out

**Files:**
- Modify: `server/main.go`

- [ ] **Step 1: Replace the TODO in `broadcast`**

```go
func (s *Server) broadcast(changes []nell.Record) {
	if len(changes) == 0 {
		return
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	msg := map[string]any{
		"changes": changes,
	}

	for addr, p := range s.peers {
		go func(p *peerConn, addr string) {
			p.mu.Lock()
			defer p.mu.Unlock()
			err := p.conn.WriteJSON(msg)
			if err != nil {
				slog.Error("[broadcast] write failed", "peer", p.nodeID, "addr", addr, "err", err)
			}
		}(p, addr)
	}
	slog.Info("[broadcast] pushed records", "peers", len(s.peers), "count", len(changes))
}
```

### Task 3: Wire up `MeshManager`

**Files:**
- Modify: `server/peer.go`

- [ ] **Step 1: Replace the TODO in `BroadcastMutation`**

Update the method in `server/peer.go`:

```go
// BroadcastMutation pushes a local mutation to all connected WebSocket peers.
func (pm *MeshManager) BroadcastMutation(rec nell.Record) {
	pm.srv.broadcast([]nell.Record{rec})
}
```

- [ ] **Step 2: Compile and test**

Run: `go test -v ./server/...`
Run: `go build -o bin/nelldb-server ./cmd/nelldb-server/`

- [ ] **Step 3: Commit**

```bash
git add server/main.go server/peer.go go.mod go.sum
git commit -m "feat(server): implement WebSocket broadcast fan-out for real-time push"
```
