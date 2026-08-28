package httpx

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
)

// Service relationships, and what looks like dead weight.
//
// This answers two questions an operator actually asks about a VPS:
//
//	"what talks to what?"  and  "what is running that nothing needs?"
//
// The first is a graph built only from evidence already in the snapshot — a
// reverse-proxy upstream, a shared Docker network, a Compose project, a socket
// attributed to a process. It never invents a relationship.
//
// The second is a set of HEURISTICS, and they are treated as exactly that.
// Every finding carries the evidence behind it and an honest confidence, and
// the whole response ships the analysis's own blind spots in `limitations`.
// Nothing here proposes a command, and nothing acts: Pulse observes and the
// operator decides. A wrongly-flagged service that someone deletes is a far
// worse outcome than one Pulse never flagged, so the rules below are
// deliberately conservative and infrastructure is exempt outright.

// --- tunables ---------------------------------------------------------------
// These thresholds decide what "idle" means. They are intentionally generous:
// the cost of a false "unused" is much higher than the cost of a miss.
const (
	// idleCPUPercent — below this a workload is doing no meaningful compute in
	// the sampled instant.
	idleCPUPercent = 0.5
	// idleTrafficBytes — total bytes in+out since the container started. A
	// service anything actually uses passes this within minutes.
	idleTrafficBytes = 4 << 20 // 4 MiB
	// residentMemoryFloor — below this, reclaiming the workload buys nothing
	// worth telling anyone about.
	residentMemoryFloor = 32 << 20 // 32 MiB
	// notableDiskBytes — the smallest disk figure worth surfacing on its own.
	notableDiskBytes = 64 << 20 // 64 MiB
)

// essentialNames are never flagged as unnecessary, whatever the numbers say.
// Two groups: the machine's own plumbing, and Pulse itself — which would
// otherwise look idle precisely because it is the thing doing the watching.
var essentialNames = map[string]bool{
	"sshd": true, "ssh": true, "systemd-journald": true, "systemd-logind": true,
	"systemd-networkd": true, "systemd-resolved": true, "systemd-udevd": true,
	"dbus": true, "cron": true, "crond": true, "rsyslog": true, "chronyd": true,
	"ntpd": true, "systemd-timesyncd": true, "docker": true, "containerd": true,
	"ufw": true, "firewalld": true, "fail2ban": true, "unattended-upgrades": true,
}

// essentialPrefixes catch the same idea for names that carry an instance
// suffix, plus every component of the Pulse stack.
var essentialPrefixes = []string{
	"pulse-", "systemd-", "getty@", "user@", "docker-", "containerd-",
}

// --- response shapes --------------------------------------------------------

type serviceRelation struct {
	From string `json:"from"`
	To   string `json:"to"`
	// Kind names the evidence: proxy_route | docker_network | compose |
	// depends_on | port. There is no other way an edge gets created.
	Kind   string `json:"kind"`
	Detail string `json:"detail,omitempty"`
}

type serviceUsage struct {
	CPUPercent  float64 `json:"cpu_percent"`
	MemoryBytes int64   `json:"memory_bytes"`
	DiskBytes   int64   `json:"disk_bytes"`
	NetRxBytes  int64   `json:"net_rx_bytes"`
	NetTxBytes  int64   `json:"net_tx_bytes"`
}

type serviceNode struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	Kind        string       `json:"kind"`      // container | service | database | proxy
	Placement   string       `json:"placement"` // host | container
	Status      string       `json:"status,omitempty"`
	Health      string       `json:"health,omitempty"`
	Image       string       `json:"image,omitempty"`
	Engine      string       `json:"engine,omitempty"`
	Project     string       `json:"project,omitempty"`
	Ports       []int        `json:"ports,omitempty"`
	PublicPorts []int        `json:"public_ports,omitempty"`
	Usage       serviceUsage `json:"usage"`
	// InboundRoutes are the domains or proxies that send traffic here.
	InboundRoutes []string `json:"inbound_routes,omitempty"`
	// Peers are other services this one is connected to, by any edge kind.
	Peers []string `json:"peers,omitempty"`
	// Essential marks infrastructure that is exempt from every waste rule.
	Essential bool `json:"essential"`
}

type auditFinding struct {
	ID          string `json:"id"`
	Subject     string `json:"subject"`
	SubjectName string `json:"subject_name"`
	Category    string `json:"category"`
	Severity    string `json:"severity"`   // info | low | medium
	Confidence  string `json:"confidence"` // low | medium | high
	Title       string `json:"title"`
	Detail      string `json:"detail"`
	// Evidence is the plain list of observations behind the finding, so the
	// operator can judge it rather than take it on faith.
	Evidence       []string     `json:"evidence"`
	Reclaimable    serviceUsage `json:"reclaimable"`
	Recommendation string       `json:"recommendation"`
}

type auditTotals struct {
	Services            int   `json:"services"`
	Relations           int   `json:"relations"`
	Flagged             int   `json:"flagged"`
	ReclaimableMemory   int64 `json:"reclaimable_memory_bytes"`
	ReclaimableDisk     int64 `json:"reclaimable_disk_bytes"`
	UnroutedServices    int   `json:"unrouted_services"`
	StoppedWithDiskUsed int   `json:"stopped_with_disk"`
}

type serviceAuditResponse struct {
	Hostname    string            `json:"hostname"`
	GeneratedAt string            `json:"generated_at"`
	Nodes       []serviceNode     `json:"nodes"`
	Relations   []serviceRelation `json:"relations"`
	Findings    []auditFinding    `json:"findings"`
	Totals      auditTotals       `json:"totals"`
	// Limitations states what this analysis cannot see. It is part of the
	// response, not a footnote, because acting on a finding without knowing
	// these is how a needed service gets removed.
	Limitations []string `json:"limitations"`
}

func (s *Server) handleServiceAudit(w http.ResponseWriter, r *http.Request) {
	p := s.principal(r)
	snap, err := s.loadSnapshot(p.OrgID, r.PathValue("id"))
	if err != nil {
		Fail(w, r, http.StatusNotFound, CodeNotFound, "no discovery data for this server yet")
		return
	}
	JSON(w, http.StatusOK, buildServiceAudit(snap))
}

// buildServiceAudit assembles the graph, then runs the waste rules over it.
func buildServiceAudit(snap *agentSnapshot) serviceAuditResponse {
	g := newServiceGraph(snap)
	findings := g.findings()

	totals := auditTotals{
		Services:  len(g.order),
		Relations: len(g.relations),
		Flagged:   len(findings),
	}
	for _, f := range findings {
		totals.ReclaimableMemory += f.Reclaimable.MemoryBytes
		totals.ReclaimableDisk += f.Reclaimable.DiskBytes
		switch f.Category {
		case "unrouted":
			totals.UnroutedServices++
		case "stopped":
			totals.StoppedWithDiskUsed++
		}
	}

	nodes := make([]serviceNode, 0, len(g.order))
	for _, id := range g.order {
		n := g.nodes[id]
		sort.Strings(n.Peers)
		sort.Strings(n.InboundRoutes)
		nodes = append(nodes, *n)
	}
	sort.SliceStable(nodes, func(i, j int) bool {
		if nodes[i].Kind != nodes[j].Kind {
			return nodes[i].Kind < nodes[j].Kind
		}
		return nodes[i].Name < nodes[j].Name
	})

	return serviceAuditResponse{
		Hostname:    snap.Hostname,
		GeneratedAt: snap.GeneratedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
		Nodes:       nodes,
		Relations:   g.relations,
		Findings:    findings,
		Totals:      totals,
		Limitations: limitations(g),
	}
}

// limitations reports the blind spots that apply to THIS snapshot, so the text
// is true rather than boilerplate.
func limitations(g *serviceGraph) []string {
	out := []string{
		"CPU is a single sample from the last discovery run, not an average — a service busy between polls looks idle here.",
		"Container network counters are cumulative since the container started, so a long-running service with brief real use can still look quiet.",
		"Traffic that never crosses a socket Pulse can see (unix sockets, shared volumes, host-network peers) produces no relationship.",
	}
	if !g.sawProxy {
		out = append(out, "No reverse proxy was discovered, so \"nothing routes to this\" carries little weight on this host.")
	}
	if g.unattributedPorts > 0 {
		out = append(out, fmt.Sprintf(
			"%d listening socket(s) could not be attributed to a process, so their owners may look less connected than they are.",
			g.unattributedPorts))
	}
	if !g.sawStorage {
		out = append(out, "Docker storage data was unavailable, so disk figures are missing and nothing is ranked by reclaimable disk.")
	}
	return out
}

// --- graph ------------------------------------------------------------------

type serviceGraph struct {
	// snap is kept so rules can reach resources that are not services in their
	// own right — orphaned volume groups, for instance.
	snap *agentSnapshot

	nodes     map[string]*serviceNode
	order     []string
	relations []serviceRelation

	byName      map[string]string // service name -> node id
	byPort      map[int]string    // host port -> node id
	byContainer map[string]string // container id -> node id

	sawProxy          bool
	sawStorage        bool
	unattributedPorts int
}

func newServiceGraph(snap *agentSnapshot) *serviceGraph {
	g := &serviceGraph{
		snap:        snap,
		nodes:       map[string]*serviceNode{},
		byName:      map[string]string{},
		byPort:      map[int]string{},
		byContainer: map[string]string{},
	}
	g.addContainers(snap)
	g.addHostServices(snap)
	g.addDatabases(snap)
	g.addProxies(snap)
	g.attachStorage(snap)
	g.attachPorts(snap)
	g.linkProxyRoutes(snap)
	g.linkContainers(snap)
	g.dedupeRelations()
	return g
}

func (g *serviceGraph) put(n serviceNode) *serviceNode {
	if existing, ok := g.nodes[n.ID]; ok {
		return existing
	}
	n.Essential = n.Essential || isEssential(n.Name)
	cp := n
	g.nodes[n.ID] = &cp
	g.order = append(g.order, n.ID)
	g.byName[strings.ToLower(n.Name)] = n.ID
	return &cp
}

func (g *serviceGraph) relate(from, to, kind, detail string) {
	if from == "" || to == "" || from == to {
		return
	}
	g.relations = append(g.relations, serviceRelation{From: from, To: to, Kind: kind, Detail: detail})
	if a, ok := g.nodes[from]; ok {
		a.Peers = appendUnique(a.Peers, to)
	}
	if b, ok := g.nodes[to]; ok {
		b.Peers = appendUnique(b.Peers, from)
	}
}

func (g *serviceGraph) addContainers(snap *agentSnapshot) {
	for _, c := range resourcesOf(snap, "docker_container") {
		cid := strings.TrimPrefix(c.ID, "container:")
		n := g.put(serviceNode{
			ID:        c.ID,
			Name:      c.Name,
			Kind:      "container",
			Placement: "container",
			Status:    c.Status,
			Health:    c.Health,
			Image:     stringAttr(c.Attributes, "image"),
			Project:   stringAttr(c.Attributes, "compose_project"),
			Usage: serviceUsage{
				CPUPercent:  floatAttr(c.Attributes, "cpu_percent"),
				MemoryBytes: int64Attr(c.Attributes, "memory_bytes"),
				NetRxBytes:  int64Attr(c.Attributes, "net_rx_bytes"),
				NetTxBytes:  int64Attr(c.Attributes, "net_tx_bytes"),
			},
		})
		for _, port := range c.Ports {
			if port.Host > 0 {
				n.Ports = appendUniqueInt(n.Ports, port.Host)
				g.byPort[port.Host] = n.ID
			}
			if port.Container > 0 {
				n.Ports = appendUniqueInt(n.Ports, port.Container)
			}
		}
		g.byContainer[cid] = n.ID
	}
}

func (g *serviceGraph) addHostServices(snap *agentSnapshot) {
	for _, u := range resourcesOf(snap, "systemd_unit", "initd_service") {
		if u.Status != "running" {
			continue
		}
		g.put(serviceNode{
			ID:        u.ID,
			Name:      u.Name,
			Kind:      "service",
			Placement: "host",
			Status:    u.Status,
			Health:    u.Health,
		})
	}
}

func (g *serviceGraph) addDatabases(snap *agentSnapshot) {
	for _, db := range resourcesOf(snap, "database") {
		// A containerised engine is already represented by its container.
		if cid := stringAttr(db.Attributes, "container_id"); cid != "" {
			if id, ok := g.byContainer[cid]; ok {
				if n := g.nodes[id]; n.Engine == "" {
					n.Engine = stringAttr(db.Attributes, "engine")
				}
				continue
			}
		}
		n := g.put(serviceNode{
			ID:        db.ID,
			Name:      db.Name,
			Kind:      "database",
			Placement: "host",
			Status:    db.Status,
			Health:    db.Health,
			Engine:    stringAttr(db.Attributes, "engine"),
		})
		for _, port := range db.Ports {
			if port.Host > 0 {
				n.Ports = appendUniqueInt(n.Ports, port.Host)
				if _, taken := g.byPort[port.Host]; !taken {
					g.byPort[port.Host] = n.ID
				}
			}
		}
	}
}

func (g *serviceGraph) addProxies(snap *agentSnapshot) {
	for _, rp := range resourcesOf(snap, "reverse_proxy") {
		g.sawProxy = true
		n := g.put(serviceNode{
			ID:        rp.ID,
			Name:      rp.Name,
			Kind:      "proxy",
			Placement: "host",
			Health:    rp.Health,
			Engine:    stringAttr(rp.Attributes, "engine"),
		})
		// A reverse proxy is load-bearing by definition; never flag it.
		n.Essential = true
	}
}

func (g *serviceGraph) attachStorage(snap *agentSnapshot) {
	for _, cs := range resourcesOf(snap, "container_storage") {
		g.sawStorage = true
		if id, ok := g.byName[strings.ToLower(cs.Name)]; ok {
			g.nodes[id].Usage.DiskBytes = int64Attr(cs.Attributes, "writable_bytes")
		}
	}
	if len(resourcesOf(snap, "docker_storage")) > 0 {
		g.sawStorage = true
	}
}

// attachPorts records which sockets belong to which service, and counts the
// ones nobody could be found for.
func (g *serviceGraph) attachPorts(snap *agentSnapshot) {
	for _, lp := range resourcesOf(snap, "listening_port") {
		if len(lp.Ports) == 0 {
			continue
		}
		port := lp.Ports[0]
		exposure := stringAttr(lp.Attributes, "exposure")

		id := ""
		if cid := stringAttr(lp.Attributes, "container_id"); cid != "" {
			id = g.byContainer[cid]
		}
		if id == "" {
			if unit := stringAttr(lp.Attributes, "unit"); unit != "" {
				id = g.byName[strings.ToLower(unit)]
			}
		}
		if id == "" {
			if proc := stringAttr(lp.Attributes, "process"); proc != "" {
				id = g.byName[strings.ToLower(proc)]
			}
		}
		if id == "" {
			if stringAttr(lp.Attributes, "process") == "" {
				g.unattributedPorts++
			}
			continue
		}
		n := g.nodes[id]
		n.Ports = appendUniqueInt(n.Ports, port.Host)
		if exposure == "public" {
			n.PublicPorts = appendUniqueInt(n.PublicPorts, port.Host)
		}
		if _, taken := g.byPort[port.Host]; !taken {
			g.byPort[port.Host] = id
		}
	}
}

// linkProxyRoutes turns every vhost upstream into an edge, which is the single
// strongest signal that a service is actually wanted.
func (g *serviceGraph) linkProxyRoutes(snap *agentSnapshot) {
	for _, vh := range resourcesOf(snap, "nginx_vhost", "apache_vhost", "caddy_site", "traefik_router", "haproxy_frontend") {
		g.sawProxy = true
		for _, up := range toStringSlice(vh.Attributes["upstreams"]) {
			target := g.resolveUpstream(up)
			if target == "" {
				continue
			}
			n := g.nodes[target]
			n.InboundRoutes = appendUnique(n.InboundRoutes, vh.Name)
			engine := stringAttr(vh.Attributes, "engine")
			if engine == "" {
				engine = strings.TrimSuffix(vh.Type, "_vhost")
			}
			// Only relate to a proxy that is itself a node; the vhost name is
			// already recorded on the target as an inbound route.
			if proxy, ok := g.nodes["reverse_proxy:"+engine]; ok {
				g.relate(proxy.ID, target, "proxy_route", vh.Name+" → "+up)
			}
		}
	}
}

// resolveUpstream maps a proxy target — "127.0.0.1:3000", "app:8080",
// "unix:/run/app.sock" — onto a service node, or "" when nothing matches.
func (g *serviceGraph) resolveUpstream(upstream string) string {
	u := strings.TrimSpace(upstream)
	u = strings.TrimPrefix(u, "http://")
	u = strings.TrimPrefix(u, "https://")
	if i := strings.IndexByte(u, '/'); i >= 0 {
		u = u[:i]
	}
	host, portStr, hasPort := strings.Cut(u, ":")

	// A named upstream is usually a container or compose service name.
	if host != "" && host != "127.0.0.1" && host != "localhost" && host != "0.0.0.0" {
		if id, ok := g.byName[strings.ToLower(host)]; ok {
			return id
		}
	}
	if hasPort {
		if port, err := strconv.Atoi(portStr); err == nil {
			if id, ok := g.byPort[port]; ok {
				return id
			}
		}
	}
	return ""
}

// linkContainers connects containers that share a user-defined network or a
// Compose project — both are declared intent that they work together.
func (g *serviceGraph) linkContainers(snap *agentSnapshot) {
	byNetwork := map[string][]string{}
	byProject := map[string][]string{}

	for _, c := range resourcesOf(snap, "docker_container") {
		id := c.ID
		for _, net := range c.Networks {
			// The default bridge joins everything and means nothing.
			if net == "bridge" || net == "host" || net == "none" {
				continue
			}
			byNetwork[net] = append(byNetwork[net], id)
		}
		if proj := stringAttr(c.Attributes, "compose_project"); proj != "" {
			byProject[proj] = append(byProject[proj], id)
		}
		for _, dep := range c.DependsOn {
			if target, ok := g.byName[strings.ToLower(dep)]; ok {
				g.relate(id, target, "depends_on", "compose depends_on")
			}
		}
	}

	for net, ids := range byNetwork {
		linkAll(g, ids, "docker_network", "shares network "+net)
	}
	for proj, ids := range byProject {
		linkAll(g, ids, "compose", "compose project "+proj)
	}
}

// linkAll relates every pair in a group. Groups are bounded in practice (a
// compose project, a user-defined network), and a wide one is skipped rather
// than turned into a hairball nobody can read.
func linkAll(g *serviceGraph, ids []string, kind, detail string) {
	const maxGroup = 24
	if len(ids) < 2 || len(ids) > maxGroup {
		return
	}
	for i := 0; i < len(ids); i++ {
		for j := i + 1; j < len(ids); j++ {
			g.relate(ids[i], ids[j], kind, detail)
		}
	}
}

func (g *serviceGraph) dedupeRelations() {
	seen := map[string]bool{}
	out := g.relations[:0]
	for _, rel := range g.relations {
		key := rel.From + "|" + rel.To + "|" + rel.Kind
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, rel)
	}
	g.relations = out
	sort.SliceStable(g.relations, func(i, j int) bool {
		if g.relations[i].From != g.relations[j].From {
			return g.relations[i].From < g.relations[j].From
		}
		return g.relations[i].To < g.relations[j].To
	})
}
