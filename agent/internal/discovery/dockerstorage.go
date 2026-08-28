package discovery

import (
	"context"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/frix-me/pulse/agent/internal/model"
)

// DockerStorageDetector answers "which projects consume disk?" using a single
// read-only GET /system/df call (the same data as `docker system df`). It sums
// container writable layers and named-volume sizes per Compose project so the
// Storage view can attribute usage to real projects, not just filesystems.
//
// /system/df computes sizes (like du), so results are cached for a few minutes
// and shared across discovery cycles to avoid repeated disk walks. Strictly
// read-only; never mutates images, containers or volumes.
type DockerStorageDetector struct {
	SocketPath string

	mu     sync.Mutex
	cached []model.Resource
	at     time.Time
}

func (*DockerStorageDetector) ID() string      { return "docker_storage" }
func (*DockerStorageDetector) Name() string    { return "Docker Storage Detector" }
func (*DockerStorageDetector) Version() string { return "1.0" }

func (d *DockerStorageDetector) socket() string {
	if d.SocketPath != "" {
		return d.SocketPath
	}
	return "/var/run/docker.sock"
}

func (d *DockerStorageDetector) Available(ctx context.Context) model.Availability {
	sock := d.socket()
	if _, err := os.Stat(sock); err != nil {
		return model.Availability{Available: false, Reason: "docker socket not found at " + sock}
	}
	return model.Availability{Available: true}
}

// systemDFResp is the subset of GET /system/df Pulse reads.
type systemDFResp struct {
	LayersSize int64 `json:"LayersSize"`
	Containers []struct {
		Names      []string          `json:"Names"`
		Image      string            `json:"Image"`
		SizeRw     int64             `json:"SizeRw"`
		SizeRootFs int64             `json:"SizeRootFs"`
		Labels     map[string]string `json:"Labels"`
	} `json:"Containers"`
	Volumes []struct {
		Name      string            `json:"Name"`
		Labels    map[string]string `json:"Labels"`
		UsageData struct {
			Size int64 `json:"Size"`
		} `json:"UsageData"`
	} `json:"Volumes"`
}

type storageAcc struct {
	project    string
	writable   int64
	volume     int64
	containers int
	volumes    int
}

func nonNeg(v int64) int64 {
	if v < 0 {
		return 0
	}
	return v
}

func (d *DockerStorageDetector) Detect(ctx context.Context) ([]model.Resource, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.cached != nil && time.Since(d.at) < 4*time.Minute {
		return d.cached, nil
	}

	c := newDockerClient(d.socket())
	var df systemDFResp
	if err := c.get(ctx, "/system/df", &df); err != nil {
		return nil, err
	}

	accs := map[string]*storageAcc{}
	get := func(proj string) *storageAcc {
		if proj == "" {
			proj = "standalone"
		}
		a := accs[proj]
		if a == nil {
			a = &storageAcc{project: proj}
			accs[proj] = a
		}
		return a
	}

	var totalWritable, totalVolume int64
	for _, ct := range df.Containers {
		proj := ""
		if ct.Labels != nil {
			proj = ct.Labels["com.docker.compose.project"]
		}
		a := get(proj)
		a.writable += nonNeg(ct.SizeRw)
		a.containers++
		totalWritable += nonNeg(ct.SizeRw)
	}
	for _, v := range df.Volumes {
		proj := ""
		if v.Labels != nil {
			proj = v.Labels["com.docker.compose.project"]
		}
		if proj == "" {
			// Anonymous / non-compose volumes: bucket together rather than
			// scatter one group per hash.
			proj = "unlabeled volumes"
		}
		a := get(proj)
		a.volume += nonNeg(v.UsageData.Size)
		a.volumes++
		totalVolume += nonNeg(v.UsageData.Size)
	}

	groups := make([]*storageAcc, 0, len(accs))
	for _, a := range accs {
		groups = append(groups, a)
	}
	sort.SliceStable(groups, func(i, j int) bool {
		return (groups[i].writable + groups[i].volume) > (groups[j].writable + groups[j].volume)
	})

	now := time.Now().UTC()
	out := make([]model.Resource, 0, len(groups)+1)
	// Summary row first so the API/dashboard can show Docker totals at a glance.
	out = append(out, model.Resource{
		Type:       "docker_storage",
		ID:         "docker_storage:summary",
		Name:       "docker",
		Health:     model.StatusHealthy,
		DetectedBy: "docker_storage",
		DetectedAt: now,
		Attributes: map[string]any{
			"images_bytes":   nonNeg(df.LayersSize),
			"writable_bytes": totalWritable,
			"volume_bytes":   totalVolume,
			"total_bytes":    nonNeg(df.LayersSize) + totalWritable + totalVolume,
		},
	})
	// Per-container writable-layer sizes, so disk can be attributed to a single
	// workload rather than only to its project. Bounded and sorted largest
	// first: the small tail is noise nobody would act on.
	out = append(out, containerStorage(df, now)...)
	for _, a := range groups {
		out = append(out, model.Resource{
			Type:       "storage_group",
			ID:         "storage_group:" + a.project,
			Name:       a.project,
			Health:     model.StatusHealthy,
			DetectedBy: "docker_storage",
			DetectedAt: now,
			Attributes: map[string]any{
				"project":         a.project,
				"writable_bytes":  a.writable,
				"volume_bytes":    a.volume,
				"total_bytes":     a.writable + a.volume,
				"container_count": a.containers,
				"volume_count":    a.volumes,
			},
		})
	}

	d.cached = out
	d.at = now
	return out, nil
}

func (*DockerStorageDetector) Health(context.Context) model.HealthReport {
	return model.HealthReport{Status: model.StatusHealthy}
}

// maxContainerStorageRows bounds how many per-container rows a snapshot carries.
const maxContainerStorageRows = 50

// containerStorage emits one row per container with a measurable writable
// layer, keyed by container name so the service audit can attribute disk to a
// specific workload. Containers with nothing written are skipped.
func containerStorage(df systemDFResp, now time.Time) []model.Resource {
	type row struct {
		name     string
		image    string
		writable int64
		rootfs   int64
	}
	rows := make([]row, 0, len(df.Containers))
	for _, ct := range df.Containers {
		if len(ct.Names) == 0 {
			continue
		}
		writable := nonNeg(ct.SizeRw)
		if writable == 0 {
			continue
		}
		// Docker returns names with a leading slash.
		rows = append(rows, row{
			name:     strings.TrimPrefix(ct.Names[0], "/"),
			image:    ct.Image,
			writable: writable,
			rootfs:   nonNeg(ct.SizeRootFs),
		})
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].writable > rows[j].writable })
	if len(rows) > maxContainerStorageRows {
		rows = rows[:maxContainerStorageRows]
	}

	out := make([]model.Resource, 0, len(rows))
	for _, r := range rows {
		out = append(out, model.Resource{
			Type:       "container_storage",
			ID:         "container_storage:" + r.name,
			Name:       r.name,
			Health:     model.StatusHealthy,
			DetectedBy: "docker_storage",
			DetectedAt: now,
			Attributes: map[string]any{
				"container":      r.name,
				"image":          r.image,
				"writable_bytes": r.writable,
				"rootfs_bytes":   r.rootfs,
			},
		})
	}
	return out
}
