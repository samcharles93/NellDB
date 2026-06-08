// nell-server is the standalone NellDB server binary.
// It can run standalone or as part of a distributed mesh.
package main

import (
	"context"
	"encoding/hex"
	"flag"
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
)

func main() {
	addr := flag.String("addr", ":9342", "HTTP listen address")
	nodeID := flag.String("node-id", defaultNodeID(), "unique node identifier")
	dataPath := flag.String("data", "nell.db", "path to data file (LogStore)")
	inMemory := flag.Bool("in-memory", false, "use ephemeral in-memory storage")
	peersFlag := flag.String("peers", "", "comma-separated peer URLs for anti-entropy mesh")
	certFile := flag.String("cert", "", "TLS certificate PEM file (enables HTTPS)")
	keyFile := flag.String("key", "", "TLS private key PEM file (enables HTTPS)")
	authKeyFlag := flag.String("auth-key", "", "HMAC shared secret (hex-encoded, 32+ bytes); also read from NELL_AUTH_KEY env")
	metricsAddr := flag.String("metrics-addr", "", "HTTP listen address for /metrics (default: same as -addr)")
	rateLimit := flag.Float64("rate-limit", 0, "per-IP rate limit in req/s (0 = disabled)")
	rateBurst := flag.Int("rate-burst", 0, "rate limit burst size (defaults to 2x rate-limit)")
	flag.Parse()

	// Structured JSON logging to stderr.
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))

	// Resolve auth key: flag takes precedence, then env.
	authKeyHex := *authKeyFlag
	if authKeyHex == "" {
		authKeyHex = os.Getenv("NELL_AUTH_KEY")
	}
	var authSecret []byte
	if authKeyHex != "" {
		var err error
		authSecret, err = hex.DecodeString(authKeyHex)
		if err != nil {
			slog.Error("invalid auth-key", "err", err)
			os.Exit(1)
		}
		if len(authSecret) < 16 {
			slog.Error("auth-key too short", "len", len(authSecret))
			os.Exit(1)
		}
		slog.Info("HMAC auth enabled", "key_bytes", len(authSecret))
	}

	var s nell.Store
	var err error
	if *inMemory {
		s = nell.NewMemoryStore(*nodeID)
	} else {
		s, err = logstore.OpenLog(*dataPath, *nodeID)
		if err != nil {
			slog.Error("failed to open log store", "err", err)
			os.Exit(1)
		}
		defer func() { _ = s.Close() }()
	}

	srv := server.New(s, *nodeID)

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
		for p := range strings.SplitSeq(*peersFlag, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				peers = append(peers, p)
			}
		}
	}
	pm := server.NewMeshManager(srv, peers, 30*time.Second, authSecret)
	if len(peers) > 0 {
		pm.Start()
	}

	// Wrap handler with HMAC auth if a secret is configured.
	h := srv.Handler()

	// Metrics middleware (outermost to capture all requests).
	h = m.Wrap(h)

	// Rate limiter (optional).
	if *rateLimit > 0 {
		burst := *rateBurst
		if burst <= 0 {
			burst = int(*rateLimit * 2)
		}
		rl := server.NewRateLimiter(*rateLimit, burst)
		h = rl.Middleware(h)
		slog.Info("rate limiter enabled", "rate", *rateLimit, "burst", burst)
	}

	if len(authSecret) > 0 {
		h = server.HMACAuth(authSecret)(h)
	}

	// Serve /metrics on a separate port or the same port.
	if *metricsAddr != "" && *metricsAddr != *addr {
		go func() {
			mux := http.NewServeMux()
			mux.Handle("/metrics", m.Handler())
			slog.Info("metrics endpoint", "addr", *metricsAddr)
			if err := http.ListenAndServe(*metricsAddr, mux); err != nil {
				slog.Error("metrics server error", "err", err)
			}
		}()
	} else {
		// Register /metrics on the main mux by wrapping the handler.
		mainMux := http.NewServeMux()
		mainMux.Handle("/metrics", m.Handler())
		mainMux.Handle("/", h)
		h = mainMux
	}

	httpSrv := &http.Server{
		Addr:         *addr,
		Handler:      h,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Start server in background so we can wait for signals on the main goroutine.
	go func() {
		useTLS := *certFile != "" && *keyFile != ""
		scheme := "http"
		if useTLS {
			scheme = "https"
		}
		slog.Info("nell-server starting", "node", *nodeID, "scheme", scheme, "addr", *addr, "data", *dataPath)

		var err error
		if useTLS {
			tlsCfg, tlErr := server.LoadTLSConfig(*certFile, *keyFile)
			if tlErr != nil {
				slog.Error("TLS config failed", "err", tlErr)
				os.Exit(1)
			}
			httpSrv.TLSConfig = tlsCfg
			err = httpSrv.ListenAndServeTLS("", "")
		} else {
			err = httpSrv.ListenAndServe()
		}
		if err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "err", err)
			os.Exit(1)
		}
	}()

	// Graceful shutdown on SIGINT/SIGTERM
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	slog.Info("shutting down...")
	pm.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(ctx); err != nil {
		slog.Error("http shutdown error", "err", err)
	}
	_ = s.Close()
	if err := m.Shutdown(ctx); err != nil {
		slog.Error("metrics shutdown error", "err", err)
	}
	slog.Info("shutdown complete")
}

func defaultNodeID() string {
	host, _ := os.Hostname()
	if host == "" {
		host = "nell-server"
	}
	return host
}
