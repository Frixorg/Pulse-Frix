package httpx

import (
	"fmt"
	"sort"
	"strings"
)

// The waste rules.
//
// Each rule answers one question with the evidence available and says how sure
// it is. None of them ever concludes "delete this" — the strongest thing a
// finding says is "nothing observed needs this; check before you act". The
// recommendation text is written to send the operator looking, not clicking.

// findings runs every rule and returns them ranked by how much they matter.
func (g *serviceGraph) findings() []auditFinding {
	var out []auditFinding
	out = append(out, g.ruleStoppedContainers()...)
	out = append(out, g.ruleUnrouted()...)
	out = append(out, g.ruleIdle()...)
	out = append(out, g.ruleUnreferencedDatabases()...)
	out = append(out, g.ruleDuplicateEngines()...)
	out = append(out, g.ruleOrphanedStorage()...)

	sort.SliceStable(out, func(i, j int) bool {
		si, sj := severityRank(out[i].Severity), severityRank(out[j].Severity)
		if si != sj {
			return si > sj
		}
		wi := out[i].Reclaimable.DiskBytes + out[i].Reclaimable.MemoryBytes
		wj := out[j].Reclaimable.DiskBytes + out[j].Reclaimable.MemoryBytes
		if wi != wj {
			return wi > wj
		}
		return out[i].SubjectName < out[j].SubjectName
	})
	return out
}

func severityRank(s string) int {
	switch s {
	case "medium":
		return 3
	case "low":
		return 2
	default:
		return 1
	}
}

// ruleStoppedContainers — a container that is not running still holds its
// writable layer and its volumes. This is the one rule with high confidence:
// "not running" is a fact, not an inference.
func (g *serviceGraph) ruleStoppedContainers() []auditFinding {
	var out []auditFinding
	for _, id := range g.order {
		n := g.nodes[id]
		if n.Kind != "container" || n.Essential {
			continue
		}
		if n.Status == "running" || n.Status == "restarting" || n.Status == "" {
			continue
		}
		ev := []string{fmt.Sprintf("Container state is %q, not running.", n.Status)}
		if n.Usage.DiskBytes > 0 {
			ev = append(ev, fmt.Sprintf("Its writable layer holds %s.", humanBytes(n.Usage.DiskBytes)))
		}
		if n.Image != "" {
			ev = append(ev, "Image: "+n.Image+".")
		}
		if len(n.InboundRoutes) > 0 {
			ev = append(ev, "A reverse proxy still routes "+strings.Join(n.InboundRoutes, ", ")+" here, so that route is currently broken.")
		}
		out = append(out, auditFinding{
			ID:          "stopped:" + n.ID,
			Subject:     n.ID,
			SubjectName: n.Name,
			Category:    "stopped",
			Severity:    "low",
			Confidence:  "high",
			Title:       n.Name + " is not running but still occupies disk",
			Detail: "The container is stopped. It uses no CPU or memory, but its writable layer " +
				"(and any named volumes) stay on disk until someone removes them.",
			Reclaimable:    serviceUsage{DiskBytes: n.Usage.DiskBytes},
			Evidence:       ev,
			Recommendation: "Check whether this was stopped deliberately. If it is finished with, its disk is yours to reclaim; if it should be running, that is the real problem here.",
		})
	}
	return out
}

// ruleUnrouted — a running service that publishes a port nothing routes to, and
// that no other service is connected to. On a host with a reverse proxy this is
// a decent signal; without one it means very little, so the rule stays silent.
func (g *serviceGraph) ruleUnrouted() []auditFinding {
	if !g.sawProxy {
		return nil
	}
	var out []auditFinding
	for _, id := range g.order {
		n := g.nodes[id]
		if n.Essential || n.Kind == "proxy" || n.Status != "running" {
			continue
		}
		if len(n.Ports) == 0 || len(n.InboundRoutes) > 0 || len(n.Peers) > 0 {
			continue
		}
		ev := []string{
			fmt.Sprintf("Listening on %s, but no reverse-proxy vhost points at it.", portList(n.Ports)),
			"No other discovered service shares a network, Compose project or dependency with it.",
		}
		if len(n.PublicPorts) > 0 {
			ev = append(ev, fmt.Sprintf("Bound to all interfaces on %s, so it is reachable from outside without going through the proxy.", portList(n.PublicPorts)))
		}
		out = append(out, auditFinding{
			ID:          "unrouted:" + n.ID,
			Subject:     n.ID,
			SubjectName: n.Name,
			Category:    "unrouted",
			Severity:    "medium",
			Confidence:  "medium",
			Title:       "Nothing observed routes to " + n.Name,
			Detail: "This service is running and listening, but no proxy route and no other service " +
				"connects to it. It may be reached directly by an external client or by something " +
				"Pulse cannot see, so this is a lead rather than a verdict.",
			Reclaimable:    serviceUsage{CPUPercent: n.Usage.CPUPercent, MemoryBytes: n.Usage.MemoryBytes, DiskBytes: n.Usage.DiskBytes},
			Evidence:       ev,
			Recommendation: "Confirm who talks to this port before doing anything. If the answer is nobody, it is a candidate for removal.",
		})
	}
	return out
}

// ruleIdle — running, consuming memory, but doing nothing measurable. Kept at
// medium severity and low-to-medium confidence: a cron-driven worker legitimately
// looks exactly like this between runs.
func (g *serviceGraph) ruleIdle() []auditFinding {
	var out []auditFinding
	for _, id := range g.order {
		n := g.nodes[id]
		if n.Essential || n.Kind == "proxy" || n.Status != "running" {
			continue
		}
		// Only containers carry usage counters; a host unit has none to judge.
		if n.Usage.MemoryBytes < residentMemoryFloor {
			continue
		}
		traffic := n.Usage.NetRxBytes + n.Usage.NetTxBytes
		if n.Usage.CPUPercent >= idleCPUPercent || traffic >= idleTrafficBytes {
			continue
		}
		confidence := "low"
		ev := []string{
			fmt.Sprintf("CPU was %.2f%% at the last sample.", n.Usage.CPUPercent),
			fmt.Sprintf("Total network traffic since it started is %s.", humanBytes(traffic)),
			fmt.Sprintf("It is holding %s of memory.", humanBytes(n.Usage.MemoryBytes)),
		}
		if len(n.InboundRoutes) == 0 && len(n.Peers) == 0 {
			// Idle AND unconnected is a much stronger signal than idle alone.
			confidence = "medium"
			ev = append(ev, "Nothing routes to it and no other service is connected to it.")
		} else if len(n.InboundRoutes) > 0 {
			ev = append(ev, "A proxy does route "+strings.Join(n.InboundRoutes, ", ")+" here, so it is wired up — just unused so far.")
		}
		out = append(out, auditFinding{
			ID:          "idle:" + n.ID,
			Subject:     n.ID,
			SubjectName: n.Name,
			Category:    "idle",
			Severity:    "medium",
			Confidence:  confidence,
			Title:       n.Name + " is running but shows no activity",
			Detail: "Near-zero CPU and almost no network traffic since it started, while holding " +
				"memory. That fits an abandoned service — and equally fits a worker that only wakes " +
				"on a schedule, so treat it as a question, not an answer.",
			Reclaimable:    serviceUsage{CPUPercent: n.Usage.CPUPercent, MemoryBytes: n.Usage.MemoryBytes, DiskBytes: n.Usage.DiskBytes},
			Evidence:       ev,
			Recommendation: "Watch it over a longer window, or check its logs for the last time it did real work, before concluding it is unused.",
		})
	}
	return out
}

// ruleUnreferencedDatabases — a database engine nothing appears to connect to.
// Databases are the most expensive thing to get wrong, so this is info-level and
// never claims more than "no connection was observed".
func (g *serviceGraph) ruleUnreferencedDatabases() []auditFinding {
	var out []auditFinding
	for _, id := range g.order {
		n := g.nodes[id]
		if n.Essential || n.Engine == "" {
			continue
		}
		if n.Kind != "database" && n.Kind != "container" {
			continue
		}
		if len(n.Peers) > 0 || len(n.InboundRoutes) > 0 {
			continue
		}
		out = append(out, auditFinding{
			ID:          "unreferenced-db:" + n.ID,
			Subject:     n.ID,
			SubjectName: n.Name,
			Category:    "unreferenced",
			Severity:    "info",
			Confidence:  "low",
			Title:       "No service was observed connecting to " + n.Name,
			Detail: "This " + n.Engine + " instance has no discovered consumer. Application clients " +
				"connect over ordinary sockets that Pulse does not trace, so a busy database can " +
				"legitimately appear here.",
			Reclaimable: serviceUsage{MemoryBytes: n.Usage.MemoryBytes, DiskBytes: n.Usage.DiskBytes},
			Evidence: []string{
				"Engine: " + n.Engine + ".",
				"No Compose project, shared network or proxy route ties it to another service.",
			},
			Recommendation: "Check the database's own connection list or its clients' configuration before treating it as unused. Never remove a database on this signal alone.",
		})
	}
	return out
}

// ruleDuplicateEngines — two of the same thing on one box is usually a leftover
// from a migration, and is worth pointing at even though either one may be live.
func (g *serviceGraph) ruleDuplicateEngines() []auditFinding {
	byEngine := map[string][]*serviceNode{}
	for _, id := range g.order {
		n := g.nodes[id]
		if n.Engine == "" || n.Status == "exited" {
			continue
		}
		byEngine[n.Engine] = append(byEngine[n.Engine], n)
	}

	engines := make([]string, 0, len(byEngine))
	for engine := range byEngine {
		engines = append(engines, engine)
	}
	sort.Strings(engines)

	var out []auditFinding
	for _, engine := range engines {
		group := byEngine[engine]
		if len(group) < 2 {
			continue
		}
		names := make([]string, 0, len(group))
		var mem, disk int64
		for _, n := range group {
			names = append(names, n.Name)
			mem += n.Usage.MemoryBytes
			disk += n.Usage.DiskBytes
		}
		sort.Strings(names)
		kind := "instances"
		severity := "info"
		if group[0].Kind == "proxy" {
			kind = "reverse proxies"
			// Two proxies both able to own :80/:443 is worth more attention.
			severity = "low"
		}
		out = append(out, auditFinding{
			ID:          "duplicate:" + engine,
			Subject:     group[0].ID,
			SubjectName: strings.Join(names, ", "),
			Category:    "duplicate",
			Severity:    severity,
			Confidence:  "medium",
			Title:       fmt.Sprintf("%d %s %s are running", len(group), engine, kind),
			Detail: "More than one " + engine + " is present on this host. That is deliberate in some " +
				"setups and a leftover from a migration in many others.",
			Reclaimable: serviceUsage{MemoryBytes: mem, DiskBytes: disk},
			Evidence: []string{
				"Instances: " + strings.Join(names, ", ") + ".",
				"Pulse cannot tell which one your applications actually use.",
			},
			Recommendation: "Confirm which instance is live before retiring the other. The reclaimable figure above is the total across all of them, not what one removal would free.",
		})
	}
	return out
}

// ruleOrphanedStorage — Docker volumes belonging to a project with no
// containers left. This is pure reclaimable disk with no service behind it.
func (g *serviceGraph) ruleOrphanedStorage() []auditFinding {
	if g.snap == nil {
		return nil
	}
	var out []auditFinding
	for _, sg := range resourcesOf(g.snap, "storage_group") {
		volumes := intAttr(sg.Attributes, "volume_count")
		containers := intAttr(sg.Attributes, "container_count")
		bytes := int64Attr(sg.Attributes, "total_bytes")
		if volumes == 0 || containers > 0 || bytes < notableDiskBytes {
			continue
		}
		out = append(out, auditFinding{
			ID:          "orphan-storage:" + sg.Name,
			Subject:     sg.ID,
			SubjectName: sg.Name,
			Category:    "orphaned",
			Severity:    "low",
			Confidence:  "medium",
			Title:       fmt.Sprintf("%s holds %s of volumes with no containers left", sg.Name, humanBytes(bytes)),
			Detail: "Every container in this project is gone, but its named volumes are still on " +
				"disk. Docker keeps them on purpose — they usually hold the data the project was for.",
			Reclaimable: serviceUsage{DiskBytes: bytes},
			Evidence: []string{
				fmt.Sprintf("%d volume(s), %s total.", volumes, humanBytes(bytes)),
				"No container in this project is present.",
			},
			Recommendation: "This is the project's data, not scratch space. Back it up or confirm it is genuinely finished with before reclaiming the disk.",
		})
	}
	return out
}

// --- small helpers ----------------------------------------------------------

func portList(ports []int) string {
	if len(ports) == 0 {
		return "no port"
	}
	sorted := append([]int(nil), ports...)
	sort.Ints(sorted)
	parts := make([]string, 0, len(sorted))
	for _, p := range sorted {
		parts = append(parts, fmt.Sprintf("%d", p))
	}
	return "port " + strings.Join(parts, ", ")
}

// humanBytes formats a byte count the way the dashboard does, so the evidence
// text and the numbers next to it agree.
func humanBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit && exp < 4; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTP"[exp])
}

func appendUnique(list []string, v string) []string {
	for _, existing := range list {
		if existing == v {
			return list
		}
	}
	return append(list, v)
}

func appendUniqueInt(list []int, v int) []int {
	if v <= 0 {
		return list
	}
	for _, existing := range list {
		if existing == v {
			return list
		}
	}
	return append(list, v)
}

// isEssential reports whether a name is infrastructure that must never be
// flagged as unnecessary, however idle it looks.
func isEssential(name string) bool {
	n := strings.ToLower(strings.TrimSuffix(name, ".service"))
	if essentialNames[n] {
		return true
	}
	for _, prefix := range essentialPrefixes {
		if strings.HasPrefix(n, prefix) {
			return true
		}
	}
	return false
}

// floatAttr reads a numeric attribute as a float. JSON numbers decode as
// float64, so that is the only shape that ever arrives here.
func floatAttr(attrs map[string]any, key string) float64 {
	if attrs == nil {
		return 0
	}
	f, _ := attrs[key].(float64)
	return f
}

func int64Attr(attrs map[string]any, key string) int64 {
	return int64(floatAttr(attrs, key))
}
