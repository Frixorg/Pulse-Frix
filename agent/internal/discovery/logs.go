package discovery

import (
	"context"
	"encoding/binary"
	"strings"
)

// LogEntry is a single collected log line (already timestamped by Docker).
type LogEntry struct {
	Source  string `json:"source"` // container name
	Stream  string `json:"stream"` // stdout | stderr
	Time    string `json:"time"`   // RFC3339 timestamp
	Message string `json:"message"`
}

// CollectContainerLogs fetches the last `tail` lines from each RUNNING container.
// It is read-only (Docker's /logs endpoint) and never writes to the container.
func CollectContainerLogs(ctx context.Context, socket string, tail int) []LogEntry {
	if socket == "" {
		socket = "/var/run/docker.sock"
	}
	c := newDockerClient(socket)
	containers, err := c.containers(ctx)
	if err != nil {
		return nil
	}
	var out []LogEntry
	for _, ct := range containers {
		if ct.State != "running" {
			continue
		}
		name := containerName(ct.Names)
		raw, err := c.logs(ctx, ct.ID, tail)
		if err != nil {
			continue
		}
		for _, fr := range demuxDockerLog(raw) {
			ts, msg := splitLogTimestamp(fr.text)
			out = append(out, LogEntry{Source: name, Stream: fr.stream, Time: ts, Message: msg})
		}
	}
	return out
}

type logFrame struct {
	stream string
	text   string
}

// demuxDockerLog splits Docker's multiplexed log stream (8-byte frame headers)
// into stdout/stderr lines, falling back to raw text for TTY containers.
func demuxDockerLog(data []byte) []logFrame {
	var frames []logFrame
	i, multiplexed := 0, false
	for i+8 <= len(data) {
		st := data[i]
		if (st == 1 || st == 2) && data[i+1] == 0 && data[i+2] == 0 && data[i+3] == 0 {
			multiplexed = true
			size := int(binary.BigEndian.Uint32(data[i+4 : i+8]))
			if size <= 0 || i+8+size > len(data) {
				break
			}
			stream := "stdout"
			if st == 2 {
				stream = "stderr"
			}
			payload := string(data[i+8 : i+8+size])
			for _, ln := range strings.Split(strings.TrimRight(payload, "\n"), "\n") {
				if ln != "" {
					frames = append(frames, logFrame{stream, ln})
				}
			}
			i += 8 + size
		} else {
			break
		}
	}
	if !multiplexed {
		for _, ln := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
			if ln != "" {
				frames = append(frames, logFrame{"stdout", ln})
			}
		}
	}
	return frames
}

// splitLogTimestamp peels off the RFC3339 timestamp Docker prepends (timestamps=1).
func splitLogTimestamp(line string) (ts, msg string) {
	if i := strings.IndexByte(line, ' '); i > 0 {
		cand := line[:i]
		if strings.Contains(cand, "T") && (strings.HasSuffix(cand, "Z") || strings.Contains(cand, "+") || strings.Count(cand, ":") >= 2) {
			return cand, line[i+1:]
		}
	}
	return "", line
}
