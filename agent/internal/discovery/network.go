package discovery

import (
	"context"
	"strings"
	"time"

	"github.com/frix-me/pulse/agent/internal/model"
)

// NetworkDetector reports network interfaces and their traffic counters by
// reading /proc/net/dev and /sys/class/net (read-only).
type NetworkDetector struct{}

func (NetworkDetector) ID() string      { return "network" }
func (NetworkDetector) Name() string    { return "Network Detector" }
func (NetworkDetector) Version() string { return "1.0" }

func (NetworkDetector) Available(context.Context) model.Availability {
	if fileExists("/proc/net/dev") {
		return model.Availability{Available: true}
	}
	return model.Availability{Available: false, Reason: "/proc/net/dev not present"}
}

func (NetworkDetector) Detect(context.Context) ([]model.Resource, error) {
	var out []model.Resource
	for _, iface := range parseNetDev() {
		if iface.Name == "lo" {
			continue
		}
		operstate := readTrim("/sys/class/net/" + iface.Name + "/operstate")
		health := model.StatusHealthy
		if operstate == "down" {
			health = model.StatusDown
		}
		out = append(out, model.Resource{
			Type:       "network_interface",
			ID:         "iface:" + iface.Name,
			Name:       iface.Name,
			Health:     health,
			DetectedBy: "network",
			DetectedAt: time.Now().UTC(),
			Attributes: map[string]any{
				"rx_bytes":   iface.RxBytes,
				"tx_bytes":   iface.TxBytes,
				"rx_packets": iface.RxPackets,
				"tx_packets": iface.TxPackets,
				"rx_errors":  iface.RxErrors,
				"tx_errors":  iface.TxErrors,
				"rx_drops":   iface.RxDrops,
				"tx_drops":   iface.TxDrops,
				"operstate":  operstate,
			},
		})
	}
	return out, nil
}

func (NetworkDetector) Health(context.Context) model.HealthReport {
	return model.HealthReport{Status: model.StatusHealthy}
}

// Iface holds counters for one network interface.
type Iface struct {
	Name                                 string
	RxBytes, RxPackets, RxErrors, RxDrops uint64
	TxBytes, TxPackets, TxErrors, TxDrops uint64
}

func parseNetDev() []Iface {
	var out []Iface
	lines := readLines("/proc/net/dev")
	for i, line := range lines {
		if i < 2 { // two header lines
			continue
		}
		name, rest, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) < 16 {
			continue
		}
		out = append(out, Iface{
			Name:      strings.TrimSpace(name),
			RxBytes:   uatoi(fields[0]),
			RxPackets: uatoi(fields[1]),
			RxErrors:  uatoi(fields[2]),
			RxDrops:   uatoi(fields[3]),
			TxBytes:   uatoi(fields[8]),
			TxPackets: uatoi(fields[9]),
			TxErrors:  uatoi(fields[10]),
			TxDrops:   uatoi(fields[11]),
		})
	}
	return out
}

func uatoi(s string) uint64 {
	var n uint64
	for _, c := range strings.TrimSpace(s) {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + uint64(c-'0')
	}
	return n
}
