// Command pulse is the Pulse CLI. It exposes read-only, non-destructive
// operations for inspecting a VPS and checking the Pulse installation.
//
//	pulse discover [--json]   run the discovery engine (read-only) and print findings
//	pulse doctor              environment + health checks
//	pulse status              what Pulse is running
//	pulse version             version + protocol
//	pulse help                usage
//
// Destructive-sounding commands (install/uninstall/rollback) are implemented by
// the installer and delegate to it; the CLI never performs privileged mutation
// on its own.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/frix-me/pulse/agent/internal/config"
	"github.com/frix-me/pulse/agent/internal/discovery"
	"github.com/frix-me/pulse/agent/internal/model"
	"github.com/frix-me/pulse/agent/internal/version"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	switch os.Args[1] {
	case "discover":
		cmdDiscover(ctx, os.Args[2:])
	case "doctor":
		cmdDoctor(ctx)
	case "status":
		cmdStatus()
	case "version", "--version", "-v":
		fmt.Printf("pulse %s (commit %s, built %s, protocol %s)\n", version.Version, version.Commit, version.BuildDate, version.Protocol)
	case "help", "--help", "-h":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", os.Args[1])
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Print(`Pulse — non-destructive VPS observability

Usage:
  pulse discover [--json]   Run read-only discovery and print findings
  pulse doctor              Run environment and health checks
  pulse status              Show what Pulse is running
  pulse version             Print version and protocol
  pulse help                Show this help

Pulse observes first and changes nothing by default. See docs/SAFETY_MODEL.md.
`)
}

func newEngine() *discovery.Engine {
	var installID, serverID string
	if cfg, err := config.Load(); err == nil {
		installID = cfg.Identity.InstallationID
		serverID = cfg.Identity.ServerID
	}
	return discovery.New(discovery.DefaultDetectors(),
		discovery.WithTimeout(10*time.Second),
		discovery.WithIdentity(installID, serverID),
	)
}

func cmdDiscover(ctx context.Context, args []string) {
	jsonOut := false
	for _, a := range args {
		if a == "--json" {
			jsonOut = true
		}
	}
	snap := newEngine().Run(ctx)

	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(snap)
		return
	}
	printSummary(snap)
}

func printSummary(snap *model.Snapshot) {
	counts := snap.CountByType()
	fmt.Println("VPS DISCOVERY")
	fmt.Println()
	fmt.Printf("Hostname: %s\n", snap.Hostname)
	if sys := firstOfType(snap, "system"); sys != nil {
		fmt.Printf("OS: %v %v\n", sys.Attributes["os"], sys.Attributes["os_version"])
		fmt.Printf("Kernel: %v\n", sys.Attributes["kernel"])
		fmt.Printf("Architecture: %v\n", sys.Attributes["architecture"])
		fmt.Printf("CPU cores: %v\n", sys.Attributes["cpu_cores"])
	}
	fmt.Println()
	fmt.Println("Detectors:")
	for _, d := range snap.Detectors {
		state := "✓ available"
		if !d.Available {
			state = "– unavailable (" + d.Reason + ")"
		}
		fmt.Printf("  %-22s %-30s %d found  %dms\n", d.ID, state, d.Count, d.DurationMS)
	}
	fmt.Println()
	fmt.Println("Resources:")
	for _, typ := range sortedKeys(counts) {
		fmt.Printf("  %-22s %d\n", typ, counts[typ])
	}
	fmt.Println()
	fmt.Printf("Discovery completed in %dms. Nothing was modified.\n", snap.DurationMS)
}

func cmdDoctor(ctx context.Context) {
	fmt.Println("Pulse Doctor")
	fmt.Println()
	snap := newEngine().Run(ctx)

	check := func(ok bool, label, detail string) {
		mark := "✓"
		if !ok {
			mark = "✗"
		}
		if detail != "" {
			fmt.Printf("  %s %s (%s)\n", mark, label, detail)
		} else {
			fmt.Printf("  %s %s\n", mark, label)
		}
	}

	byID := map[string]model.DetectorResult{}
	for _, d := range snap.Detectors {
		byID[d.ID] = d
	}

	check(true, "OS supported", fmt.Sprintf("%v", attr(snap, "system", "os")))
	check(byID["docker"].Available, "Docker available", byID["docker"].Reason)
	check(byID["systemd"].Available, "systemd available", byID["systemd"].Reason)
	check(byID["network"].Available, "network available", "")
	check(byID["filesystem"].Available, "filesystem readable", byID["filesystem"].Reason)
	check(byID["ports"].Available, "port table readable", byID["ports"].Reason)

	// disk pressure
	diskOK := true
	for _, r := range snap.Resources {
		if r.Type == "filesystem" && r.Health == model.StatusDown {
			diskOK = false
		}
	}
	check(diskOK, "disk space sufficient", "")

	fmt.Println()
	if snap.DurationMS >= 0 {
		fmt.Printf("Doctor completed in %dms. All checks are read-only.\n", snap.DurationMS)
	}
}

func cmdStatus() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not load config: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Pulse Status")
	fmt.Println()
	fmt.Printf("Mode:            %s\n", cfg.Mode)
	fmt.Printf("Server ID:       %s\n", cfg.Identity.ServerID)
	fmt.Printf("Agent ID:        %s\n", cfg.Identity.AgentID)
	fmt.Printf("Installation ID: %s\n", cfg.Identity.InstallationID)
	if cfg.Mode == config.ModeCloud {
		fmt.Printf("API URL:         %s\n", cfg.APIURL)
	}
	fmt.Printf("Data dir:        %s\n", cfg.DataDir)
}

// helpers

func firstOfType(snap *model.Snapshot, typ string) *model.Resource {
	for i := range snap.Resources {
		if snap.Resources[i].Type == typ {
			return &snap.Resources[i]
		}
	}
	return nil
}

func attr(snap *model.Snapshot, typ, key string) any {
	if r := firstOfType(snap, typ); r != nil {
		return r.Attributes[key]
	}
	return ""
}

func sortedKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// simple insertion sort to avoid importing sort for a tiny slice
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j-1] > keys[j]; j-- {
			keys[j-1], keys[j] = keys[j], keys[j-1]
		}
	}
	return keys
}
