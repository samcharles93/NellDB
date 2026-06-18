package server

import (
	"errors"
	"net"
	"testing"

	"github.com/hashicorp/mdns"
)

// ── NewMDNSDiscoverer ──────────────────────────────────────────────────────

func TestNewMDNSDiscoverer(t *testing.T) {
	t.Run("default service name when empty", func(t *testing.T) {
		d := NewMDNSDiscoverer(MDNSConfig{
			Port:   8080,
			NodeID: "node-1",
		})
		if d == nil {
			t.Fatal("NewMDNSDiscoverer returned nil")
		}
		if d.cfg.ServiceName != "_nell-core._tcp" {
			t.Errorf("expected default service name '_nell-core._tcp', got %q", d.cfg.ServiceName)
		}
		if d.cfg.Port != 8080 {
			t.Errorf("expected port 8080, got %d", d.cfg.Port)
		}
		if d.cfg.NodeID != "node-1" {
			t.Errorf("expected NodeID 'node-1', got %q", d.cfg.NodeID)
		}
	})

	t.Run("custom service name preserved", func(t *testing.T) {
		d := NewMDNSDiscoverer(MDNSConfig{
			ServiceName: "_myapp._tcp",
			Port:        9000,
			NodeID:      "node-2",
		})
		if d.cfg.ServiceName != "_myapp._tcp" {
			t.Errorf("expected '_myapp._tcp', got %q", d.cfg.ServiceName)
		}
	})

	t.Run("stopCh initialized", func(t *testing.T) {
		d := NewMDNSDiscoverer(MDNSConfig{Port: 8080})
		if d.stopCh == nil {
			t.Error("stopCh should not be nil")
		}
	})
}

// ── peerURLFromEntry ───────────────────────────────────────────────────────

func TestPeerURLFromEntry(t *testing.T) {
	tests := []struct {
		name  string
		entry *mdns.ServiceEntry
		want  string
	}{
		{
			name: "port zero returns empty",
			entry: &mdns.ServiceEntry{
				Host:   "some-host.local",
				Port:   0,
				AddrV4: net.ParseIP("192.168.1.10"),
			},
			want: "",
		},
		{
			name: "empty host and no addr returns empty",
			entry: &mdns.ServiceEntry{
				Host:   "",
				Port:   8080,
				AddrV4: nil,
				Addr:   nil,
			},
			want: "",
		},
		{
			name: "AddrV4 preferred over Host",
			entry: &mdns.ServiceEntry{
				Host:   "some-host.local",
				Port:   8080,
				AddrV4: net.ParseIP("192.168.1.10"),
			},
			want: "http://192.168.1.10:8080",
		},
		{
			name: "deprecated Addr used when AddrV4 is nil",
			entry: &mdns.ServiceEntry{
				Host:   "some-host.local",
				Port:   8080,
				AddrV4: nil,
				Addr:   net.ParseIP("10.0.0.5"),
			},
			want: "http://10.0.0.5:8080",
		},
		{
			name: "Host used when both AddrV4 and Addr are nil",
			entry: &mdns.ServiceEntry{
				Host:   "my-host.local",
				Port:   9090,
				AddrV4: nil,
				Addr:   nil,
			},
			want: "http://my-host.local:9090",
		},
		{
			name: "different port",
			entry: &mdns.ServiceEntry{
				Host:   "peer.local",
				Port:   443,
				AddrV4: net.ParseIP("10.0.0.1"),
			},
			want: "http://10.0.0.1:443",
		},
		{
			name: "Host is empty but AddrV4 present",
			entry: &mdns.ServiceEntry{
				Host:   "",
				Port:   3000,
				AddrV4: net.ParseIP("172.16.0.1"),
			},
			want: "http://172.16.0.1:3000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := peerURLFromEntry(tt.entry)
			if got != tt.want {
				t.Errorf("peerURLFromEntry() = %q, want %q", got, tt.want)
			}
		})
	}
}

// ── localIPs ───────────────────────────────────────────────────────────────

func TestLocalIPs(t *testing.T) {
	ips := localIPs()
	if ips == nil {
		t.Skip("no network interfaces available on this machine")
	}
	if len(ips) == 0 {
		t.Skip("no non-loopback IPv4 addresses found on this machine")
	}
	for _, ip := range ips {
		if ip.IsLoopback() {
			t.Errorf("localIPs() returned loopback IP: %s", ip.String())
		}
		if ip.To4() == nil {
			t.Errorf("localIPs() returned non-IPv4 address: %s", ip.String())
		}
	}
	t.Logf("found %d non-loopback IPv4 address(es)", len(ips))
	for _, ip := range ips {
		t.Logf("  %s", ip.String())
	}
}

// ── isMulticastErr ─────────────────────────────────────────────────────────

func TestIsMulticastErr(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil error", nil, false},
		{"no such device", errors.New("listen udp: no such device"), true},
		{"cannot assign", errors.New("socket: cannot assign requested address"), true},
		{"network is unreachable", errors.New("dial: network is unreachable"), true},
		{"no route to host", errors.New("connect: no route to host"), true},
		{"unrelated error", errors.New("connection refused"), false},
		{"empty error", errors.New(""), false},
		{"partial substring not enough", errors.New("no device here"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isMulticastErr(tt.err)
			if got != tt.want {
				t.Errorf("isMulticastErr(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
