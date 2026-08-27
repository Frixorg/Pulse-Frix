package httpx

import (
	"net/http"
	"sort"
	"strings"
)

// The unified inventory.
//
// Every detector answers one question well — Docker knows containers, systemd
// knows units, the port detector knows sockets — but an operator asks a
// different one: "what is running on this box, and is it on the host or in a
// container?". This handler is that correlation, done server-side from the
// single snapshot the agent already sends, so the dashboard needs one request.
//
// Listening sockets are attached to whatever owns them: a container id, a
// systemd unit, or a bare PID. Anything that could not be attributed is
// reported separately rather than dropped — an unattributed listener is a real
// finding, not an empty result. It usually means the agent lacked permission to
// read /proc/<pid>/fd.

type inventoryPort struct {
	Port     int    `json:"port"`
	Protocol string `json:"protocol"`
	Address  string `json:"address,omitempty"`
	Exposure string `json:"exposure,omitempty"`
}

type inventoryItem struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Kind is what the thing is: container | service | database | proxy.
	Kind string `json:"kind"`
	// Placement is where it runs: "host" or "container". This is the split the
	// inventory exists to make explicit.
	Placement   string          `json:"placement"`
	Status      string          `json:"status,omitempty"`
	Health      string          `json:"health,omitempty"`
	Engine      string          `json:"engine,omitempty"`
	Image       string          `json:"image,omitempty"`
	Unit        string          `json:"unit,omitempty"`
	PID         int             `json:"pid,omitempty"`
	ContainerID string          `json:"container_id,omitempty"`
	Ports       []inventoryPort `json:"ports,omitempty"`
	DetectedBy  string          `json:"detected_by,omitempty"`
}

type inventoryTotals struct {
	HostWorkloads      int `json:"host_workloads"`
	ContainerWorkloads int `json:"container_workloads"`
	ListeningPorts     int `json:"listening_ports"`
	PublicPorts        int `json:"public_ports"`
	Databases          int `json:"databases"`
	Unattributed       int `json:"unattributed_ports"`
}

type inventoryResponse struct {
	Hostname     string          `json:"hostname"`
	GeneratedAt  string          `json:"generated_at"`
	Totals       inventoryTotals `json:"totals"`
	Items        []inventoryItem `json:"items"`
	Unattributed []inventoryPort `json:"unattributed"`
}

func (s *Server) handleInventory(w http.ResponseWriter, r *http.Request) {
	p := s.principal(r)
	snap, err := s.loadSnapshot(p.OrgID, r.PathValue("id"))
	if err != nil {
		Fail(w, r, http.StatusNotFound, CodeNotFound, "no inventory data for this server yet")
		return
	}
	JSON(w, http.StatusOK, buildInventory(snap))
}

// buildInventory correlates the snapshot's resources into one workload list.
func buildInventory(snap *agentSnapshot) inventoryResponse {
	items := map[string]*inventoryItem{}
	var order []string

	put := func(item inventoryItem) *inventoryItem {
		if existing, ok := items[item.ID]; ok {
			return existing
		}
		cp := item
		items[item.ID] = &cp
		order = append(order, item.ID)
		return &cp
	}

	// Containers first: they are the most specific thing a port can belong to.
	byContainerID := map[string]*inventoryItem{}
	for _, c := range resourcesOf(snap, "docker_container") {
		cid := strings.TrimPrefix(c.ID, "container:")
		item := put(inventoryItem{
			ID:          c.ID,
			Name:        c.Name,
			Kind:        "container",
			Placement:   "container",
			Status:      c.Status,
			Health:      c.Health,
			Image:       stringAttr(c.Attributes, "image"),
			ContainerID: cid,
			DetectedBy:  c.DetectedBy,
		})
		for _, port := range c.Ports {
			if port.Host > 0 {
				item.Ports = append(item.Ports, inventoryPort{
					Port: port.Host, Protocol: port.Protocol, Address: port.Address,
				})
			}
		}
		byContainerID[cid] = item
	}

	// Host services: systemd units and SysV scripts that are actually running.
	byUnit := map[string]*inventoryItem{}
	for _, u := range resourcesOf(snap, "systemd_unit", "initd_service") {
		if u.Status != "running" && u.Status != "failed" {
			continue
		}
		item := put(inventoryItem{
			ID:         u.ID,
			Name:       u.Name,
			Kind:       "service",
			Placement:  "host",
			Status:     u.Status,
			Health:     u.Health,
			PID:        intAttr(u.Attributes, "pid"),
			Unit:       u.Name,
			DetectedBy: u.DetectedBy,
		})
		byUnit[u.Name] = item
	}

	// Reverse proxies are host workloads worth naming in their own right.
	for _, rp := range resourcesOf(snap, "reverse_proxy") {
		put(inventoryItem{
			ID:         rp.ID,
			Name:       rp.Name,
			Kind:       "proxy",
			Placement:  "host",
			Health:     rp.Health,
			Engine:     stringAttr(rp.Attributes, "engine"),
			DetectedBy: rp.DetectedBy,
		})
	}

	// Databases: a containerised one folds into its container, a host one
	// stands on its own.
	databases := 0
	for _, db := range resourcesOf(snap, "database") {
		databases++
		cid := stringAttr(db.Attributes, "container_id")
		if owner, ok := byContainerID[cid]; ok && cid != "" {
			if owner.Engine == "" {
				owner.Engine = stringAttr(db.Attributes, "engine")
			}
			continue
		}
		placement := "host"
		if stringAttr(db.Attributes, "workload") == "container" {
			placement = "container"
		}
		item := put(inventoryItem{
			ID:          db.ID,
			Name:        db.Name,
			Kind:        "database",
			Placement:   placement,
			Status:      db.Status,
			Health:      db.Health,
			Engine:      stringAttr(db.Attributes, "engine"),
			Unit:        stringAttr(db.Attributes, "unit"),
			PID:         intAttr(db.Attributes, "pid"),
			ContainerID: cid,
			DetectedBy:  db.DetectedBy,
		})
		for _, port := range db.Ports {
			if port.Host > 0 {
				item.Ports = append(item.Ports, inventoryPort{Port: port.Host, Protocol: port.Protocol})
			}
		}
	}

	// Finally the sockets. Each is attached to its owner, or reported as
	// unattributed so an unexplained open port is never silently lost.
	totals := inventoryTotals{Databases: databases}
	var unattributed []inventoryPort
	for _, lp := range resourcesOf(snap, "listening_port") {
		if len(lp.Ports) == 0 {
			continue
		}
		port := lp.Ports[0]
		exposure := stringAttr(lp.Attributes, "exposure")
		totals.ListeningPorts++
		if exposure == "public" {
			totals.PublicPorts++
		}
		ip := inventoryPort{
			Port: port.Host, Protocol: port.Protocol, Address: port.Address, Exposure: exposure,
		}

		if cid := stringAttr(lp.Attributes, "container_id"); cid != "" {
			if owner, ok := byContainerID[cid]; ok {
				owner.Ports = appendUniquePort(owner.Ports, ip)
				continue
			}
		}
		if unit := stringAttr(lp.Attributes, "unit"); unit != "" {
			if owner, ok := byUnit[unit]; ok {
				owner.Ports = appendUniquePort(owner.Ports, ip)
				continue
			}
		}
		// A named process with no unit or container is still a host workload.
		if proc := stringAttr(lp.Attributes, "process"); proc != "" {
			owner := put(inventoryItem{
				ID:         "process:" + proc,
				Name:       proc,
				Kind:       "service",
				Placement:  "host",
				Status:     "running",
				Health:     "HEALTHY",
				PID:        intAttr(lp.Attributes, "pid"),
				DetectedBy: lp.DetectedBy,
			})
			owner.Ports = appendUniquePort(owner.Ports, ip)
			continue
		}
		unattributed = append(unattributed, ip)
	}
	totals.Unattributed = len(unattributed)

	out := make([]inventoryItem, 0, len(order))
	for _, id := range order {
		item := items[id]
		sort.Slice(item.Ports, func(i, j int) bool { return item.Ports[i].Port < item.Ports[j].Port })
		if item.Placement == "container" {
			totals.ContainerWorkloads++
		} else {
			totals.HostWorkloads++
		}
		out = append(out, *item)
	}
	// Containers first, then host services, each alphabetically — a stable
	// order keeps the dashboard from reshuffling on every poll.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Placement != out[j].Placement {
			return out[i].Placement == "container"
		}
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Name < out[j].Name
	})
	if unattributed == nil {
		unattributed = []inventoryPort{}
	}

	return inventoryResponse{
		Hostname:     snap.Hostname,
		GeneratedAt:  snap.GeneratedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
		Totals:       totals,
		Items:        out,
		Unattributed: unattributed,
	}
}

// resourcesOf is the package-level twin of Server.resourcesOfType, so the
// inventory builder can be exercised without a Server.
func resourcesOf(snap *agentSnapshot, types ...string) []agentResource {
	want := map[string]bool{}
	for _, t := range types {
		want[t] = true
	}
	var out []agentResource
	for _, r := range snap.Resources {
		if want[r.Type] {
			out = append(out, r)
		}
	}
	return out
}

func stringAttr(attrs map[string]any, key string) string {
	if attrs == nil {
		return ""
	}
	s, _ := attrs[key].(string)
	return s
}

// intAttr reads a numeric attribute. JSON numbers decode as float64, so that is
// the only shape that ever arrives here.
func intAttr(attrs map[string]any, key string) int {
	if attrs == nil {
		return 0
	}
	if f, ok := attrs[key].(float64); ok {
		return int(f)
	}
	return 0
}

func appendUniquePort(ports []inventoryPort, p inventoryPort) []inventoryPort {
	for _, existing := range ports {
		if existing.Port == p.Port && existing.Protocol == p.Protocol {
			return ports
		}
	}
	return append(ports, p)
}
