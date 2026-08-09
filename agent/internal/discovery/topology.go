package discovery

import (
	"sort"

	"github.com/frix-me/pulse/agent/internal/model"
)

// BuildTopology derives an infrastructure graph from REAL discovered resources.
// It never invents relationships: every edge is backed by an nginx upstream, a
// docker network, a compose project, or an explicit dependency. See
// docs/DISCOVERY.md#topology--dependencies.
func BuildTopology(resources []model.Resource) *model.Topology {
	topo := &model.Topology{}
	seen := map[string]bool{}

	addNode := func(id, label, typ string, health model.Status) {
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		topo.Nodes = append(topo.Nodes, model.TopoNode{ID: id, Label: label, Type: typ, Health: health})
	}
	addEdge := func(from, to, source string) {
		if from == "" || to == "" || from == to {
			return
		}
		topo.Edges = append(topo.Edges, model.TopoEdge{From: from, To: to, Source: source})
	}

	hasReverseProxy := false
	for _, r := range resources {
		switch r.Type {
		case "reverse_proxy", "nginx_vhost", "caddy_site", "traefik_router", "apache_vhost":
			hasReverseProxy = true
		}
	}
	if hasReverseProxy {
		addNode("internet", "Internet", "internet", "")
	}

	for _, r := range resources {
		switch r.Type {
		case "nginx_vhost", "apache_vhost", "caddy_site", "traefik_router", "reverse_proxy":
			node := "proxy:" + r.Name
			addNode(node, r.Name, "reverse_proxy", r.Health)
			addEdge("internet", node, "reverse_proxy")
			// upstream targets recorded on the resource become edges
			if ups, ok := r.Attributes["upstreams"].([]string); ok {
				for _, u := range ups {
					target := "upstream:" + u
					addNode(target, u, "upstream", "")
					addEdge(node, target, "nginx_upstream")
				}
			}
		case "docker_container":
			node := "container:" + r.Name
			addNode(node, r.Name, "container", r.Health)
			for _, net := range r.Networks {
				netNode := "network:" + net
				addNode(netNode, net, "network", "")
				addEdge(node, netNode, "docker_network")
			}
			for _, dep := range r.DependsOn {
				addEdge(node, "container:"+dep, "compose")
			}
		case "database":
			node := "db:" + r.Name
			addNode(node, r.Name, "database", r.Health)
		}
	}

	sort.SliceStable(topo.Nodes, func(i, j int) bool { return topo.Nodes[i].ID < topo.Nodes[j].ID })
	sort.SliceStable(topo.Edges, func(i, j int) bool {
		if topo.Edges[i].From != topo.Edges[j].From {
			return topo.Edges[i].From < topo.Edges[j].From
		}
		return topo.Edges[i].To < topo.Edges[j].To
	})
	if len(topo.Nodes) == 0 {
		return nil
	}
	return topo
}
