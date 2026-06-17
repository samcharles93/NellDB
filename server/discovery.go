package server

import (
	"fmt"
	"log/slog"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/hashicorp/mdns"
)

// MDNSConfig holds configuration for mDNS peer discovery.
type MDNSConfig struct {
	Enabled     bool   // Enable mDNS discovery (default: false)
	ServiceName string // mDNS service name (default: "_nell-core._tcp")
	Port        int    // Port to advertise
	NodeID      string // Node identifier for TXT record
}

// MDNSDiscoverer advertises a NellDB node via mDNS and discovers peers
// on the local network.  Discovered peers are forwarded to MeshManager.AddPeer.
//
// On platforms that lack multicast (Docker, WSL, some cloud VMs), the
// discoverer logs a warning and disables itself — it never panics or
// blocks startup.
type MDNSDiscoverer struct {
	cfg    MDNSConfig
	server *mdns.Server
	stopCh chan struct{}
	mu     sync.Mutex
}

// NewMDNSDiscoverer creates an mDNS discoverer.  It does not start
// advertising or browsing until Start is called.
func NewMDNSDiscoverer(cfg MDNSConfig) *MDNSDiscoverer {
	if cfg.ServiceName == "" {
		cfg.ServiceName = "_nell-core._tcp"
	}
	return &MDNSDiscoverer{
		cfg:    cfg,
		stopCh: make(chan struct{}),
	}
}

// Start begins advertising this node and browsing for peers.  The addPeer
// callback is invoked for each discovered peer (on a background goroutine).
// Safe to call only once.
func (d *MDNSDiscoverer) Start(addPeer func(string)) error {
	if !d.cfg.Enabled {
		return nil
	}

	host, _ := os.Hostname()
	info := []string{
		"node_id=" + d.cfg.NodeID,
		"port=" + strconv.Itoa(d.cfg.Port),
		"version=1",
	}

	// ── Advertise ────────────────────────────────────────────────────
	srv, err := mdns.NewServer(&mdns.Config{
		Zone: &mdns.MDNSService{
			Instance: d.cfg.NodeID,
			Service:  d.cfg.ServiceName,
			Domain:   "local",
			HostName: host,
			Port:     d.cfg.Port,
			IPs:      localIPs(),
			TXT:      info,
		},
	})
	if err != nil {
		if isMulticastErr(err) {
			slog.Warn("[discovery] mDNS unavailable — multicast not supported on this platform; peer discovery disabled")
			d.cfg.Enabled = false
			return nil
		}
		return fmt.Errorf("mDNS advertise: %w", err)
	}
	d.mu.Lock()
	d.server = srv
	d.mu.Unlock()

	slog.Info("[discovery] mDNS advertising started", "service", d.cfg.ServiceName, "node", d.cfg.NodeID, "port", d.cfg.Port)

	// ── Browse ───────────────────────────────────────────────────────
	go d.browse(addPeer)

	return nil
}

// Stop halts advertising and browsing.
func (d *MDNSDiscoverer) Stop() {
	d.mu.Lock()
	defer d.mu.Unlock()
	select {
	case <-d.stopCh:
		return // already stopped
	default:
		close(d.stopCh)
	}
	if d.server != nil {
		d.server.Shutdown()
	}
}

// browse discovers peers on the local network and forwards them to addPeer.
func (d *MDNSDiscoverer) browse(addPeer func(string)) {
	entriesCh := make(chan *mdns.ServiceEntry, 16)

	// Consumer goroutine: drains entries until entriesCh is closed.
	go func() {
		for entry := range entriesCh {
			url := peerURLFromEntry(entry)
			if url != "" {
				slog.Info("[discovery] found peer", "url", url, "node", entry.Info)
				addPeer(url)
			}
		}
	}()

	params := &mdns.QueryParam{
		Service:             d.cfg.ServiceName,
		Domain:              "local",
		Timeout:             0, // continuous browsing
		Entries:             entriesCh,
		DisableIPv4:         false,
		DisableIPv6:         false,
		WantUnicastResponse: false,
	}

	slog.Info("[discovery] mDNS browsing started", "service", d.cfg.ServiceName)

	// mdns.Query blocks until the timeout expires or an error occurs.
	// We run it in a goroutine and wait for it to return before closing entriesCh.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := mdns.Query(params); err != nil {
			if !isMulticastErr(err) {
				slog.Error("[discovery] mDNS browse error", "err", err)
			}
		}
	}()

	<-d.stopCh
	wg.Wait()        // wait for Query goroutine to return before closing entriesCh
	close(entriesCh) // safe — no writer remains
	slog.Info("[discovery] mDNS browsing stopped")
}

// peerURLFromEntry extracts an HTTP URL from an mDNS service entry.
func peerURLFromEntry(entry *mdns.ServiceEntry) string {
	if entry.Port == 0 {
		return ""
	}
	addr := entry.Host
	// If we have an IPv4 address, prefer it.
	if entry.AddrV4 != nil {
		addr = entry.AddrV4.String()
	} else if entry.Addr != nil {
		addr = entry.Addr.String()
	}
	if addr == "" {
		return ""
	}
	// Use HTTP by default (not HTTPS).  TLS can be enabled separately
	// in production deployments.
	return fmt.Sprintf("http://%s:%d", addr, entry.Port)
}

// localIPs returns all non-loopback IPv4 addresses of the host.
func localIPs() []net.IP {
	var ips []net.IP
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagMulticast == 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok {
				if ip4 := ipnet.IP.To4(); ip4 != nil && !ip4.IsLoopback() {
					ips = append(ips, ip4)
				}
			}
		}
	}
	return ips
}

// isMulticastErr returns true if the error indicates multicast is unavailable
// (e.g., Docker, WSL, or cloud VMs without multicast support).
func isMulticastErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	for _, sub := range []string{
		"no such device",
		"cannot assign",
		"network is unreachable",
		"no route to host",
	} {
		if strings.Contains(msg, sub) {
			return true
		}
	}
	return false
}
