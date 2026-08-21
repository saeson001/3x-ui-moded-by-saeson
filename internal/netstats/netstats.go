// Package netstats distinguishes VPS NIC traffic from Xray proxy traffic.
//
// The 3x-ui panel only counts bytes that flow through Xray (inbounds table
// up/down), while the VPS provider bills every byte on the NIC. This module
// samples both counters and exposes per-second rates for:
//   - net    : whole-NIC traffic (all processes, what the provider bills)
//   - xray   : traffic that went through Xray inbounds (what the panel shows)
//   - system : net - xray, i.e. everything NOT handled by the proxy
//              (SSH brute-force, port scans, other services, protocol overhead)
package netstats

import (
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/net"
	"gorm.io/gorm"
)

// Rates in bytes/sec, Totals in bytes.
type NetStats struct {
	Net struct {
		Up   uint64 `json:"up"`
		Down uint64 `json:"down"`
		Sent uint64 `json:"sent"`
		Recv uint64 `json:"recv"`
	} `json:"net"`
	Xray struct {
		Up       uint64 `json:"up"`
		Down     uint64 `json:"down"`
		UpTotal   uint64 `json:"upTotal"`
		DownTotal uint64 `json:"downTotal"`
	} `json:"xray"`
	System struct {
		Up   uint64 `json:"up"`
		Down uint64 `json:"down"`
	} `json:"system"`
}

type sampler struct {
	mu          sync.Mutex
	hasSample   bool
	lastTime    time.Time
	lastSent    uint64
	lastRecv    uint64
	lastXrayUp  uint64
	lastXrayDn  uint64
}

var defaultSampler = &sampler{}

// isVirtualInterface mirrors upstream 3x-ui server.go so that our NIC totals
// match the panel's "total data" card exactly.
func isVirtualInterface(name string) bool {
	if name == "lo" || name == "lo0" {
		return true
	}
	prefixes := []string{
		"loopback", "docker", "br-", "veth", "virbr",
		"tun", "tap", "wg", "tailscale", "zt",
	}
	for _, p := range prefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

func nicTotals() (sent, recv uint64, err error) {
	ioStats, err := net.IOCounters(true)
	if err != nil {
		return 0, 0, err
	}
	for _, iface := range ioStats {
		if isVirtualInterface(strings.ToLower(iface.Name)) {
			continue
		}
		sent += iface.BytesSent
		recv += iface.BytesRecv
	}
	return sent, recv, nil
}

// xrayTotals sums up/down over every inbound in the panel database.
// This is the same counter the panel UI shows per inbound.
func xrayTotals(db *gorm.DB) (up, down uint64, err error) {
	var row struct {
		Up   *int64
		Down *int64
	}
	if err := db.Raw("SELECT COALESCE(SUM(up),0) AS up, COALESCE(SUM(down),0) AS down FROM inbounds").Scan(&row).Error; err != nil {
		return 0, 0, err
	}
	if row.Up != nil {
		up = uint64(*row.Up)
	}
	if row.Down != nil {
		down = uint64(*row.Down)
	}
	return up, down, nil
}

// GetStats returns current rates/totals. Thread-safe; called on each
// /panel/api/netstats request. Rates are computed against the previous call,
// so the frontend should poll at a fixed interval (it already polls status).
func GetStats(db *gorm.DB) (*NetStats, error) {
	sent, recv, err := nicTotals()
	if err != nil {
		return nil, err
	}
	xrayUp, xrayDown, err := xrayTotals(db)
	if err != nil {
		return nil, err
	}

	s := defaultSampler
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	out := &NetStats{}
	out.Net.Sent = sent
	out.Net.Recv = recv
	out.Xray.UpTotal = xrayUp
	out.Xray.DownTotal = xrayDown

	if s.hasSample {
		seconds := now.Sub(s.lastTime).Seconds()
		if seconds > 0.2 {
			if sent >= s.lastSent {
				out.Net.Up = uint64(float64(sent-s.lastSent) / seconds)
			}
			if recv >= s.lastRecv {
				out.Net.Down = uint64(float64(recv-s.lastRecv) / seconds)
			}
			if xrayUp >= s.lastXrayUp {
				out.Xray.Up = uint64(float64(xrayUp-s.lastXrayUp) / seconds)
			}
			if xrayDown >= s.lastXrayDn {
				out.Xray.Down = uint64(float64(xrayDown-s.lastXrayDn) / seconds)
			}
			// system = NIC - Xray (proxy traffic is a subset of NIC traffic).
			// Xray counts application-layer bytes; NIC includes TCP/IP + TLS
			// overhead, so the diff may slightly over-count "system" traffic,
			// and clamping guards against negative values on counter resets.
			if out.Net.Up >= out.Xray.Up {
				out.System.Up = out.Net.Up - out.Xray.Up
			}
			if out.Net.Down >= out.Xray.Down {
				out.System.Down = out.Net.Down - out.Xray.Down
			}
		}
	}

	s.lastTime = now
	s.lastSent = sent
	s.lastRecv = recv
	s.lastXrayUp = xrayUp
	s.lastXrayDn = xrayDown
	s.hasSample = true

	return out, nil
}
