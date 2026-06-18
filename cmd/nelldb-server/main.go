// nell-server is the standalone NellDB server binary.
// It can run standalone or as part of a distributed mesh.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/samcharles93/NellDB"
	"github.com/samcharles93/NellDB/logstore"
	"github.com/samcharles93/NellDB/server"
	"github.com/klauspost/compress/zstd"
)

func main() {
	// 1. Initial flags (including config path)
	configPath := flag.String("config", "nell.yaml", "path to nell.yaml configuration")
	addr := flag.String("addr", "", "HTTP listen address (overrides config)")
	nodeID := flag.String("node-id", "", "unique node identifier (overrides config)")
	dataPath := flag.String("data", "", "path to data file (overrides config)")
	inMemory := flag.Bool("in-memory", false, "use ephemeral in-memory storage")
	peersFlag := flag.String("peers", "", "comma-separated peer URLs for anti-entropy mesh")
	discoveryFlag := flag.Bool("discovery", false, "enable mDNS LAN peer discovery")
	authSecret := flag.String("auth-secret", "", "HMAC shared secret for sync endpoint auth")
	tlsCert := flag.String("tls-cert", "", "TLS certificate file path")
	tlsKey := flag.String("tls-key", "", "TLS private key file path")
	flag.Parse()

	// Structured JSON logging to stderr.
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))

	// 2. Load Configuration
	cfg := nell.DefaultConfig()
	if *configPath != "" {
		if loaded, err := nell.LoadConfig(*configPath); err == nil {
			cfg = loaded
			slog.Info("config loaded", "path", *configPath)
		} else if !os.IsNotExist(err) {
			slog.Error("failed to load config", "path", *configPath, "err", err)
			os.Exit(1)
		}
	}

	// 3. Merge Flags
	if *addr != "" {
		// addr flag overrides cfg.Server.Port if it's just a port
		if strings.HasPrefix(*addr, ":") {
			fmt.Sscanf(*addr, ":%d", &cfg.Server.Port)
		}
	} else {
		*addr = fmt.Sprintf(":%d", cfg.Server.Port)
	}
	if *nodeID == "" {
		*nodeID = defaultNodeID()
	}
	if *dataPath == "" {
		*dataPath = cfg.Server.DataDir + "/nell.db"
	}

	// 4. Initialize Store
	var s nell.Store
	var stopCompaction func()
	var err error
	if *inMemory {
		s = nell.NewMemoryStore(*nodeID)
		slog.Info("using in-memory store")
	} else {
		opts := logstore.Options{
			FlushInterval: time.Duration(cfg.Storage.FlushIntervalMs) * time.Millisecond,
		}
		if lvl, ok := compressionLevel(cfg.Storage.CompressionLevel); ok {
			opts.CompressionLevel = lvl
		}
		s, err = logstore.OpenLogWithOptions(*dataPath, *nodeID, opts)
		if err != nil {
			slog.Error("failed to open log store", "err", err)
			os.Exit(1)
		}
		defer func() { _ = s.Close() }()
		if cfg.Storage.FlushIntervalMs > 0 {
			slog.Info("group-commit enabled", "flush_interval_ms", cfg.Storage.FlushIntervalMs)
		}
		if cfg.Storage.CompressionLevel != "" && cfg.Storage.CompressionLevel != "default" {
			slog.Info("compression level", "level", cfg.Storage.CompressionLevel)
		}
	}

	// Type-assert to LogStore for auto-compaction.
	if ls, ok := s.(*logstore.LogStore); ok {
		interval := time.Duration(cfg.Compaction.IntervalMinutes) * time.Minute
		ttl := time.Duration(cfg.Compaction.TombstoneTTLHours) * time.Hour
		stopCompaction = ls.StartCompaction(context.Background(), interval, ttl)
		slog.Info("auto-compaction enabled", "interval_minutes", cfg.Compaction.IntervalMinutes, "tombstone_ttl_hours", cfg.Compaction.TombstoneTTLHours)
	}

	srv := server.New(s, *nodeID)

	// ── Auth secret ─────────────────────────────────────────────────────
	if *authSecret != "" {
		srv.SetAuthSecret([]byte(*authSecret))
		slog.Info("HMAC auth enabled for sync endpoints")
	}

	// ── Metrics ──────────────────────────────────────────────────────
	m, err := server.NewMetrics()
	if err != nil {
		slog.Error("failed to initialize metrics", "err", err)
		os.Exit(1)
	}
	srv.SetMetrics(m)

	// ── Peer mesh ───────────────────────────────────────────────────
	var peers []string
	if *peersFlag != "" {
		peers = strings.Split(*peersFlag, ",")
	}
	peers = append(peers, cfg.Peers...)
	pm := server.NewMeshManager(srv, peers, 30*time.Second, []byte(*authSecret))
	srv.SetMeshManager(pm)
	pm.Start() // always start — discovery may populate peers later

	// ── mDNS Discovery ─────────────────────────────────────────────
	discoveryEnabled := *discoveryFlag || cfg.Discovery.Enabled
	discoverer := server.NewMDNSDiscoverer(server.MDNSConfig{
		Enabled: discoveryEnabled,
		Port:    cfg.Server.Port,
		NodeID:  *nodeID,
	})
	if err := discoverer.Start(pm.AddPeer); err != nil {
		slog.Error("failed to start mDNS discovery", "err", err)
		os.Exit(1)
	}

	// ── Handler Assembly ─────────────────────────────────────────────
	h := srv.Handler()

	// Serve Web UI if enabled
	if cfg.Web.Enabled {
		mainMux := http.NewServeMux()
		mainMux.Handle("/sync/", h)
		mainMux.Handle("/health", h)
		mainMux.Handle("/ready", h)
		mainMux.Handle("/ui/", http.StripPrefix("/ui", server.WebUIHandler()))
		// Redirect root to UI
		mainMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/" {
				http.Redirect(w, r, "/ui/", http.StatusMovedPermanently)
				return
			}
			h.ServeHTTP(w, r)
		})
		h = mainMux
		slog.Info("Web UI enabled", "path", "/ui/")
	}

	// Metrics middleware
	h = m.Wrap(h)

	httpSrv := &http.Server{
		Addr:         *addr,
		Handler:      h,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Resolve TLS from flags (precedence) then config
	tlsCertFile, tlsKeyFile := *tlsCert, *tlsKey
	if tlsCertFile == "" && tlsKeyFile == "" {
		tlsCertFile, tlsKeyFile = cfg.Server.TLSCert, cfg.Server.TLSKey
	}

	// Start server
	go func() {
		slog.Info("nell-server starting", "node", *nodeID, "addr", *addr, "data", *dataPath)
		var err error
		if tlsCertFile != "" && tlsKeyFile != "" {
			slog.Info("TLS enabled", "cert", tlsCertFile)
			err = httpSrv.ListenAndServeTLS(tlsCertFile, tlsKeyFile)
		} else {
			err = httpSrv.ListenAndServe()
		}
		if err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "err", err)
			os.Exit(1)
		}
	}()

	// Graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	slog.Info("shutting down...")
	if stopCompaction != nil {
		stopCompaction()
	}
	discoverer.Stop()
	pm.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(ctx); err != nil {
		slog.Error("http shutdown error", "err", err)
	}
	_ = s.Close()
	slog.Info("shutdown complete")
}

func defaultNodeID() string {
	host, _ := os.Hostname()
	if host == "" {
		host = "nell-server"
	}
	return host
}

// compressionLevel maps a config string to the zstd encoder level.  Returns
// ok=false for empty or unknown values so the caller can fall back to default.
func compressionLevel(s string) (zstd.EncoderLevel, bool) {
	switch strings.ToLower(s) {
	case "fastest":
		return zstd.SpeedFastest, true
	case "default":
		return zstd.SpeedDefault, true
	case "better":
		return zstd.SpeedBetterCompression, true
	case "best":
		return zstd.SpeedBestCompression, true
	}
	return 0, false
}
