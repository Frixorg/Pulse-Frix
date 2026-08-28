package httpx

import (
	"strings"
	"testing"
)

// auditSnapshot is a small but realistic host: a proxy routing to an app, the
// app's database beside it on a Compose network, an abandoned container, a
// stopped one, and infrastructure that must never be flagged.
func auditSnapshot() *agentSnapshot {
	return &agentSnapshot{
		Hostname: "vps-1",
		Resources: []agentResource{
			{
				Type: "reverse_proxy", ID: "reverse_proxy:nginx", Name: "nginx",
				Health: "HEALTHY", DetectedBy: "nginx",
				Attributes: map[string]any{"engine": "nginx", "vhosts": float64(1)},
			},
			{
				Type: "nginx_vhost", ID: "nginx:app.example.com", Name: "app.example.com",
				Health: "HEALTHY", DetectedBy: "nginx",
				Attributes: map[string]any{"ssl": true, "upstreams": []any{"127.0.0.1:3000"}},
			},
			{
				Type: "docker_container", ID: "container:aaaaaaaaaaaa", Name: "app",
				Status: "running", Health: "HEALTHY", DetectedBy: "docker",
				Attributes: map[string]any{
					"image": "ghcr.io/acme/app:1.2", "compose_project": "acme",
					"cpu_percent": 4.2, "memory_bytes": float64(300 << 20),
					"net_rx_bytes": float64(90 << 20), "net_tx_bytes": float64(40 << 20),
				},
				Ports:    []agentPort{{Host: 3000, Container: 3000, Protocol: "tcp"}},
				Networks: []string{"acme_default"},
			},
			{
				Type: "docker_container", ID: "container:bbbbbbbbbbbb", Name: "acme-db",
				Status: "running", Health: "HEALTHY", DetectedBy: "docker",
				Attributes: map[string]any{
					"image": "postgres:16", "compose_project": "acme",
					"cpu_percent": 1.1, "memory_bytes": float64(200 << 20),
					"net_rx_bytes": float64(50 << 20), "net_tx_bytes": float64(50 << 20),
				},
				Networks: []string{"acme_default"},
			},
			// Running, listening, nothing routes to it, no peers, holding memory.
			{
				Type: "docker_container", ID: "container:cccccccccccc", Name: "old-staging",
				Status: "running", Health: "HEALTHY", DetectedBy: "docker",
				Attributes: map[string]any{
					"image": "ghcr.io/acme/app:0.4",
					"cpu_percent": 0.0, "memory_bytes": float64(180 << 20),
					"net_rx_bytes": float64(1200), "net_tx_bytes": float64(800),
				},
				Ports: []agentPort{{Host: 4100, Container: 3000, Protocol: "tcp"}},
			},
			// Stopped, still on disk.
			{
				Type: "docker_container", ID: "container:dddddddddddd", Name: "migration-runner",
				Status: "exited", Health: "UNKNOWN", DetectedBy: "docker",
				Attributes: map[string]any{"image": "ghcr.io/acme/migrate:1.0"},
			},
			{
				Type: "container_storage", ID: "container_storage:migration-runner", Name: "migration-runner",
				DetectedBy: "docker_storage",
				Attributes:  map[string]any{"writable_bytes": float64(700 << 20)},
			},
			// Pulse's own agent must never be flagged, however quiet it looks.
			{
				Type: "docker_container", ID: "container:eeeeeeeeeeee", Name: "pulse-agent",
				Status: "running", Health: "HEALTHY", DetectedBy: "docker",
				Attributes: map[string]any{
					"image": "pulse/agent", "cpu_percent": 0.0,
					"memory_bytes": float64(64 << 20), "net_rx_bytes": float64(10), "net_tx_bytes": float64(10),
				},
			},
			// So must sshd.
			{
				Type: "systemd_unit", ID: "systemd:sshd.service", Name: "sshd.service",
				Status: "running", Health: "HEALTHY", DetectedBy: "systemd",
			},
			{
				Type: "listening_port", ID: "port:0.0.0.0:4100/tcp", Name: "4100/tcp", DetectedBy: "ports",
				Attributes: map[string]any{"exposure": "public", "container_id": "cccccccccccc"},
				Ports:      []agentPort{{Host: 4100, Protocol: "tcp", Address: "0.0.0.0"}},
			},
			// Volumes with no containers left behind them.
			{
				Type: "storage_group", ID: "storage_group:retired", Name: "retired",
				DetectedBy: "docker_storage",
				Attributes: map[string]any{
					"project": "retired", "volume_count": float64(2), "container_count": float64(0),
					"total_bytes": float64(900 << 20),
				},
			},
		},
	}
}

func findFinding(res serviceAuditResponse, category, subjectName string) *auditFinding {
	for i := range res.Findings {
		if res.Findings[i].Category == category && res.Findings[i].SubjectName == subjectName {
			return &res.Findings[i]
		}
	}
	return nil
}

func TestServiceAuditBuildsRelationsFromEvidence(t *testing.T) {
	res := buildServiceAudit(auditSnapshot())

	app := findAuditNode(res, "app")
	if app == nil {
		t.Fatal("the app container is missing")
	}
	// The nginx upstream 127.0.0.1:3000 resolves to the container publishing 3000.
	if len(app.InboundRoutes) != 1 || app.InboundRoutes[0] != "app.example.com" {
		t.Errorf("inbound routes: got %v", app.InboundRoutes)
	}
	// app and acme-db share a Compose project and a user-defined network.
	if len(app.Peers) == 0 {
		t.Error("app should be related to its database")
	}

	var kinds = map[string]bool{}
	for _, rel := range res.Relations {
		kinds[rel.Kind] = true
	}
	for _, want := range []string{"proxy_route", "docker_network", "compose"} {
		if !kinds[want] {
			t.Errorf("expected a %q relation, got kinds %v", want, kinds)
		}
	}
}

func findAuditNode(res serviceAuditResponse, name string) *serviceNode {
	for i := range res.Nodes {
		if res.Nodes[i].Name == name {
			return &res.Nodes[i]
		}
	}
	return nil
}

func TestServiceAuditFlagsTheAbandonedService(t *testing.T) {
	res := buildServiceAudit(auditSnapshot())

	if f := findFinding(res, "unrouted", "old-staging"); f == nil {
		t.Error("a running, listening service with no route and no peers should be flagged")
	} else {
		if f.Confidence == "" || len(f.Evidence) == 0 {
			t.Error("every finding must carry evidence and a confidence")
		}
		if strings.Contains(strings.ToLower(f.Recommendation), "docker rm") ||
			strings.Contains(strings.ToLower(f.Recommendation), "delete it") {
			t.Errorf("findings must not prescribe a removal: %q", f.Recommendation)
		}
	}
	if f := findFinding(res, "idle", "old-staging"); f == nil {
		t.Error("near-zero CPU and traffic while holding memory should also read as idle")
	}
}

func TestServiceAuditFlagsStoppedAndOrphanedDisk(t *testing.T) {
	res := buildServiceAudit(auditSnapshot())

	stopped := findFinding(res, "stopped", "migration-runner")
	if stopped == nil {
		t.Fatal("a stopped container holding disk should be flagged")
	}
	if stopped.Confidence != "high" {
		t.Errorf(`"not running" is a fact, not an inference; got confidence %q`, stopped.Confidence)
	}
	if stopped.Reclaimable.DiskBytes != 700<<20 {
		t.Errorf("reclaimable disk: got %d", stopped.Reclaimable.DiskBytes)
	}

	orphan := findFinding(res, "orphaned", "retired")
	if orphan == nil {
		t.Fatal("volumes with no containers left should be flagged")
	}
	if orphan.Reclaimable.DiskBytes != 900<<20 {
		t.Errorf("reclaimable disk: got %d", orphan.Reclaimable.DiskBytes)
	}
}

// The most important test here: infrastructure must never be called unnecessary,
// however quiet it looks. Pulse's own agent is the sharpest case — it is idle by
// design, because it is the thing doing the watching.
func TestServiceAuditNeverFlagsInfrastructure(t *testing.T) {
	res := buildServiceAudit(auditSnapshot())
	for _, f := range res.Findings {
		for _, protected := range []string{"pulse-agent", "sshd.service", "nginx"} {
			if strings.Contains(f.SubjectName, protected) {
				t.Errorf("%s was flagged as %s: %s", protected, f.Category, f.Title)
			}
		}
	}
	if n := findAuditNode(res, "pulse-agent"); n == nil || !n.Essential {
		t.Error("pulse-agent should be marked essential")
	}
	if n := findAuditNode(res, "sshd.service"); n == nil || !n.Essential {
		t.Error("sshd should be marked essential")
	}
}

func TestServiceAuditDoesNotFlagAHealthyRoutedService(t *testing.T) {
	res := buildServiceAudit(auditSnapshot())
	for _, f := range res.Findings {
		if f.SubjectName == "app" {
			t.Errorf("a routed, busy service must not be flagged: %s", f.Title)
		}
	}
}

func TestServiceAuditAlwaysStatesItsLimits(t *testing.T) {
	res := buildServiceAudit(auditSnapshot())
	if len(res.Limitations) < 3 {
		t.Errorf("the response must carry its own blind spots, got %v", res.Limitations)
	}
}

func TestServiceAuditUnroutedRuleIsSilentWithoutAProxy(t *testing.T) {
	// Strip the proxy: with nothing routing anywhere, "nothing routes to this"
	// says nothing, and the rule must not fire.
	snap := auditSnapshot()
	var kept []agentResource
	for _, r := range snap.Resources {
		if r.Type == "reverse_proxy" || r.Type == "nginx_vhost" {
			continue
		}
		kept = append(kept, r)
	}
	snap.Resources = kept

	res := buildServiceAudit(snap)
	for _, f := range res.Findings {
		if f.Category == "unrouted" {
			t.Errorf("unrouted must not fire without a proxy, got %q", f.Title)
		}
	}
}

func TestServiceAuditEmptySnapshot(t *testing.T) {
	res := buildServiceAudit(&agentSnapshot{Hostname: "empty"})
	if len(res.Findings) != 0 {
		t.Errorf("nothing discovered means nothing to flag, got %v", res.Findings)
	}
	if res.Totals.Services != 0 {
		t.Errorf("services: got %d", res.Totals.Services)
	}
}

func TestHumanBytes(t *testing.T) {
	cases := map[int64]string{
		512:            "512 B",
		2048:           "2.0 KiB",
		5 << 20:        "5.0 MiB",
		3 << 30:        "3.0 GiB",
	}
	for in, want := range cases {
		if got := humanBytes(in); got != want {
			t.Errorf("%d: got %q, want %q", in, got, want)
		}
	}
}

func TestIsEssential(t *testing.T) {
	for _, name := range []string{"sshd", "sshd.service", "pulse-api", "systemd-journald.service", "docker"} {
		if !isEssential(name) {
			t.Errorf("%q should be essential", name)
		}
	}
	for _, name := range []string{"app", "old-staging", "acme-db", "wordpress"} {
		if isEssential(name) {
			t.Errorf("%q should not be essential", name)
		}
	}
}
