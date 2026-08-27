package httpx

import "testing"

// buildInventory is the correlation the whole inventory view rests on, so the
// cases below cover each way a listening socket finds (or fails to find) its
// owner: by container id, by systemd unit, by process name, and not at all.
func inventorySnapshot() *agentSnapshot {
	return &agentSnapshot{
		Hostname: "vps-1",
		Resources: []agentResource{
			{
				Type: "docker_container", ID: "container:abc123456789", Name: "app",
				Status: "running", Health: "HEALTHY", DetectedBy: "docker",
				Attributes: map[string]any{"image": "ghcr.io/acme/app:1.2"},
				Ports:      []agentPort{{Host: 8080, Container: 8080, Protocol: "tcp"}},
			},
			{
				Type: "systemd_unit", ID: "systemd:nginx.service", Name: "nginx.service",
				Status: "running", Health: "HEALTHY", DetectedBy: "systemd",
				Attributes: map[string]any{"pid": float64(812)},
			},
			{
				Type: "systemd_unit", ID: "systemd:stopped.service", Name: "stopped.service",
				Status: "inactive", Health: "UNKNOWN", DetectedBy: "systemd",
			},
			{
				Type: "database", ID: "db:postgresql:5432", Name: "postgresql",
				Status: "reachable", Health: "HEALTHY", DetectedBy: "databases",
				Attributes: map[string]any{"engine": "postgresql", "workload": "host", "unit": "postgresql.service"},
				Ports:      []agentPort{{Host: 5432, Protocol: "tcp"}},
			},
			// Sockets: one per attribution path.
			{
				Type: "listening_port", ID: "port:0.0.0.0:8080/tcp", Name: "8080/tcp", DetectedBy: "ports",
				Attributes: map[string]any{"exposure": "public", "container_id": "abc123456789"},
				Ports:      []agentPort{{Host: 8080, Protocol: "tcp", Address: "0.0.0.0"}},
			},
			{
				Type: "listening_port", ID: "port:0.0.0.0:443/tcp", Name: "443/tcp", DetectedBy: "ports",
				Attributes: map[string]any{"exposure": "public", "unit": "nginx.service"},
				Ports:      []agentPort{{Host: 443, Protocol: "tcp", Address: "0.0.0.0"}},
			},
			{
				Type: "listening_port", ID: "port:127.0.0.1:9100/tcp", Name: "9100/tcp", DetectedBy: "ports",
				Attributes: map[string]any{"exposure": "loopback", "process": "node_exporter", "pid": float64(991)},
				Ports:      []agentPort{{Host: 9100, Protocol: "tcp", Address: "127.0.0.1"}},
			},
			{
				Type: "listening_port", ID: "port:0.0.0.0:2222/tcp", Name: "2222/tcp", DetectedBy: "ports",
				Attributes: map[string]any{"exposure": "public"},
				Ports:      []agentPort{{Host: 2222, Protocol: "tcp", Address: "0.0.0.0"}},
			},
		},
	}
}

func findItem(inv inventoryResponse, name string) *inventoryItem {
	for i := range inv.Items {
		if inv.Items[i].Name == name {
			return &inv.Items[i]
		}
	}
	return nil
}

func TestBuildInventoryAttributesPorts(t *testing.T) {
	inv := buildInventory(inventorySnapshot())

	app := findItem(inv, "app")
	if app == nil {
		t.Fatal("the container is missing from the inventory")
	}
	if app.Placement != "container" {
		t.Errorf("placement: got %q", app.Placement)
	}
	// The container publishes 8080 and the socket also resolves to it; the
	// port must appear exactly once.
	if len(app.Ports) != 1 || app.Ports[0].Port != 8080 {
		t.Errorf("container ports: got %v", app.Ports)
	}

	nginx := findItem(inv, "nginx.service")
	if nginx == nil {
		t.Fatal("the running unit is missing from the inventory")
	}
	if nginx.Placement != "host" {
		t.Errorf("placement: got %q", nginx.Placement)
	}
	if len(nginx.Ports) != 1 || nginx.Ports[0].Port != 443 {
		t.Errorf("the :443 socket should attach to nginx.service, got %v", nginx.Ports)
	}

	// A socket owned by a named process with no unit still becomes a workload.
	exporter := findItem(inv, "node_exporter")
	if exporter == nil {
		t.Fatal("a process-owned socket should produce a host workload")
	}
	if exporter.Placement != "host" || len(exporter.Ports) != 1 {
		t.Errorf("got %+v", *exporter)
	}
}

func TestBuildInventorySkipsInactiveUnits(t *testing.T) {
	inv := buildInventory(inventorySnapshot())
	if findItem(inv, "stopped.service") != nil {
		t.Error("an inactive unit is not a running workload and must not be listed")
	}
}

func TestBuildInventoryReportsUnattributedPorts(t *testing.T) {
	inv := buildInventory(inventorySnapshot())
	if len(inv.Unattributed) != 1 || inv.Unattributed[0].Port != 2222 {
		t.Fatalf("the ownerless socket must be surfaced, got %v", inv.Unattributed)
	}
	if inv.Totals.Unattributed != 1 {
		t.Errorf("unattributed total: got %d", inv.Totals.Unattributed)
	}
}

func TestBuildInventoryTotals(t *testing.T) {
	inv := buildInventory(inventorySnapshot())
	if inv.Totals.ContainerWorkloads != 1 {
		t.Errorf("container workloads: got %d", inv.Totals.ContainerWorkloads)
	}
	// nginx.service, the host Postgres, and the node_exporter process.
	if inv.Totals.HostWorkloads != 3 {
		t.Errorf("host workloads: got %d", inv.Totals.HostWorkloads)
	}
	if inv.Totals.ListeningPorts != 4 {
		t.Errorf("listening ports: got %d", inv.Totals.ListeningPorts)
	}
	if inv.Totals.PublicPorts != 3 {
		t.Errorf("public ports: got %d", inv.Totals.PublicPorts)
	}
	if inv.Totals.Databases != 1 {
		t.Errorf("databases: got %d", inv.Totals.Databases)
	}
}

func TestBuildInventoryEmptySnapshot(t *testing.T) {
	inv := buildInventory(&agentSnapshot{Hostname: "empty"})
	if len(inv.Items) != 0 {
		t.Errorf("expected no items, got %v", inv.Items)
	}
	// Encoding must produce [] rather than null for an empty result.
	if inv.Unattributed == nil {
		t.Error("unattributed should be an empty slice, not nil")
	}
}

func TestTraefikHostsFromRule(t *testing.T) {
	hosts := traefikHostsFromRule("Host(`app.example.com`, `www.example.com`) && PathPrefix(`/api`)")
	if len(hosts) != 2 || hosts[0] != "app.example.com" {
		t.Errorf("got %v", hosts)
	}
	if got := traefikHostsFromRule("PathPrefix(`/api`)"); len(got) != 0 {
		t.Errorf("a rule with no Host matcher should yield nothing, got %v", got)
	}
}

func TestContainerHasTLSLabel(t *testing.T) {
	on := map[string]string{"traefik.http.routers.app.tls.certresolver": "le"}
	if !containerHasTLSLabel(on) {
		t.Error("a certresolver label means TLS is on")
	}
	off := map[string]string{"traefik.http.routers.app.rule": "Host(`a.example.com`)"}
	if containerHasTLSLabel(off) {
		t.Error("a bare rule label does not imply TLS")
	}
}
