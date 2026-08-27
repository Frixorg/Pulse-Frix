package discovery

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/frix-me/pulse/agent/internal/model"
)

// DatabaseDetector finds database instances two ways:
//
//   - By listening port, correlated with the process that owns the socket, so a
//     Postgres on 5432 is reported together with the PID, systemd unit or
//     container behind it.
//   - By open file, for engines that have no port at all — SQLite, which lives
//     entirely in a file some application holds open.
//
// Reachability is confirmed with a read-only TCP connect. No credentials are
// required, nothing is written, and no database configuration is ever touched.
// Deeper metrics come from official read-only exporters (docs/MONITORING.md).
type DatabaseDetector struct{}

func (DatabaseDetector) ID() string      { return "databases" }
func (DatabaseDetector) Name() string    { return "Database Detector" }
func (DatabaseDetector) Version() string { return "1.1" }

func (DatabaseDetector) Available(context.Context) model.Availability {
	return model.Availability{Available: true}
}

type dbSignature struct {
	engine string
	port   int
}

// knownDatabases maps a default port to its engine. Only ports that are
// overwhelmingly one engine are listed — a wrong label is worse than none.
var knownDatabases = []dbSignature{
	{"postgresql", 5432},
	{"mysql", 3306},
	{"mariadb", 3307},
	{"redis", 6379},
	{"mongodb", 27017},
	{"memcached", 11211},
	{"elasticsearch", 9200},
	{"clickhouse", 8123},
	{"cassandra", 9042},
	{"influxdb", 8086},
	{"couchdb", 5984},
	{"neo4j", 7687},
	{"cockroachdb", 26257},
	{"mssql", 1433},
	{"rabbitmq", 5672},
}

func (DatabaseDetector) Detect(context.Context) ([]model.Resource, error) {
	listeners := ListeningPortsWithOwners()
	byPort := map[int]Listener{}
	for _, l := range listeners {
		if l.Protocol == "tcp" {
			byPort[l.Port] = l
		}
	}

	var out []model.Resource
	now := time.Now().UTC()
	for _, sig := range knownDatabases {
		l, ok := byPort[sig.port]
		if !ok {
			continue
		}
		reachable := tcpProbe(fmt.Sprintf("127.0.0.1:%d", sig.port), 2*time.Second)
		health := model.StatusDown
		if reachable {
			health = model.StatusHealthy
		}
		attrs := map[string]any{
			"engine":   sig.engine,
			"exposure": l.Exposure(), // "public" here is a Security-view finding
			"workload": ternaryString(l.ContainerID != "", "container", "host"),
		}
		if l.PID > 0 {
			attrs["pid"] = l.PID
			attrs["process"] = l.Process
		}
		if l.Unit != "" {
			attrs["unit"] = l.Unit
		}
		if l.ContainerID != "" {
			attrs["container_id"] = l.ContainerID
		}
		out = append(out, model.Resource{
			Type:       "database",
			ID:         fmt.Sprintf("db:%s:%d", sig.engine, sig.port),
			Name:       sig.engine,
			Status:     ternaryString(reachable, "reachable", "unreachable"),
			Health:     health,
			DetectedBy: "databases",
			DetectedAt: now,
			Ports:      []model.Port{{Host: sig.port, Protocol: "tcp"}},
			Attributes: attrs,
		})
	}

	out = append(out, detectSQLite(now)...)
	return out, nil
}

func (DatabaseDetector) Health(context.Context) model.HealthReport {
	return model.HealthReport{Status: model.StatusHealthy}
}

func ternaryString(cond bool, a, b string) string {
	if cond {
		return a
	}
	return b
}

// --- SQLite ----------------------------------------------------------------

// sqliteExtensions are the suffixes worth inspecting. The extension only
// selects candidates; the file header is what confirms an actual database.
var sqliteExtensions = []string{".sqlite", ".sqlite3", ".db", ".db3"}

// sqliteMagic is the 16-byte header every SQLite 3 file starts with.
var sqliteMagic = []byte("SQLite format 3\x00")

// maxSQLiteReported bounds how many databases a single snapshot carries.
const maxSQLiteReported = 25

// detectSQLite finds SQLite databases that a running process holds open. This
// finds the ones that actually matter — a live application's store — without
// walking the filesystem, and every candidate is confirmed by reading its
// 16-byte header. Nothing is written and no database is opened as a database.
func detectSQLite(now time.Time) []model.Resource {
	candidates := ScanOpenFiles(looksLikeSQLitePath, maxSQLiteReported*4)
	if len(candidates) == 0 {
		return nil
	}

	procs := IndexProcesses(ScanProcesses())
	paths := make([]string, 0, len(candidates))
	for p := range candidates {
		paths = append(paths, p)
	}
	sort.Strings(paths) // deterministic output for stable diffs

	var out []model.Resource
	for _, path := range paths {
		if len(out) >= maxSQLiteReported {
			break
		}
		readable, ok := resolveSQLiteFile(path)
		if !ok {
			continue
		}
		pids := candidates[path]
		attrs := map[string]any{
			"engine": "sqlite",
			"file":   displayPath(path),
			// A file-backed database is reachable only to the processes that
			// opened it; there is no socket and therefore no exposure.
			"exposure": "file",
		}
		if info, err := os.Stat(readable); err == nil {
			attrs["size_bytes"] = info.Size()
		}
		var owners []string
		workload := "host"
		for _, pid := range pids {
			if p, ok := procs[pid]; ok {
				owners = append(owners, p.Comm)
				if p.Containerised() {
					workload = "container"
					attrs["container_id"] = p.ContainerID
				}
			}
		}
		if len(owners) > 0 {
			attrs["opened_by"] = owners
		}
		attrs["workload"] = workload

		out = append(out, model.Resource{
			Type:       "database",
			ID:         "db:sqlite:" + displayPath(path),
			Name:       filepath.Base(path),
			Status:     "in_use",
			Health:     model.StatusHealthy,
			DetectedBy: "databases",
			DetectedAt: now,
			Attributes: attrs,
		})
	}
	return out
}

func looksLikeSQLitePath(path string) bool {
	lower := strings.ToLower(path)
	// Skip the noisy system stores that are never an application's database.
	if strings.HasPrefix(path, "/proc/") || strings.HasPrefix(path, "/sys/") ||
		strings.Contains(path, "/var/lib/dpkg/") || strings.Contains(path, "/var/lib/rpm/") {
		return false
	}
	for _, ext := range sqliteExtensions {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

// resolveSQLiteFile confirms that a candidate really is a SQLite database and
// returns the path the agent can actually read it at. A containerised agent
// sees the file under the host rootfs rather than at the path the owning
// process reported, so both are tried.
func resolveSQLiteFile(path string) (string, bool) {
	for _, p := range []string{path, hostPath(path)} {
		if p != "" && hasSQLiteHeader(p) {
			return p, true
		}
	}
	return "", false
}

// hasSQLiteHeader reads the first 16 bytes only — enough to confirm the format
// without opening the database or touching its journal.
func hasSQLiteHeader(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, len(sqliteMagic))
	if _, err := io.ReadFull(f, buf); err != nil {
		return false
	}
	return bytes.Equal(buf, sqliteMagic)
}
