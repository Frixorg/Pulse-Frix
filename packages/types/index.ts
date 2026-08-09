// Shared types for the Pulse API and dashboard. These mirror the Go model
// (agent/internal/model) and the API responses (docs/API.md). Keep in sync.

export type Health = "HEALTHY" | "DEGRADED" | "DOWN" | "UNKNOWN";
export type Severity = "INFO" | "WARNING" | "CRITICAL";
export type Role = "owner" | "admin" | "viewer";
export type Mode = "local" | "cloud";

export interface Port {
  host?: number;
  container?: number;
  protocol: string;
  address?: string;
  state?: string;
}

export interface Resource {
  type: string;
  id: string;
  name: string;
  status?: string;
  health?: Health;
  labels?: Record<string, string>;
  attributes?: Record<string, unknown>;
  ports?: Port[];
  networks?: string[];
  volumes?: string[];
  depends_on?: string[];
  detected_by: string;
  detected_at: string;
}

export interface TopoNode {
  id: string;
  label: string;
  type: string;
  health?: Health;
}

export interface TopoEdge {
  from: string;
  to: string;
  source: string;
}

export interface Topology {
  nodes: TopoNode[];
  edges: TopoEdge[];
}

export interface Server {
  id: string;
  server_id: string;
  hostname: string;
  status: Health;
  mode: Mode;
  last_seen_at?: string;
}

export interface ServerSummary {
  server: Server;
  cpu_percent: number;
  mem_used_pct: number;
  disk_used_pct: number;
  net_rx_bytes: number;
  net_tx_bytes: number;
  uptime_sec: number;
  health: Health;
  counts: {
    services_healthy: number;
    services_degraded: number;
    services_down: number;
    containers_running: number;
    containers_unhealthy: number;
    domains_online: number;
    domains_ssl_expiring: number;
    alerts_critical: number;
    alerts_warning: number;
  };
}

export interface Domain {
  id: string;
  fqdn: string;
  http_status?: number;
  latency_ms?: number;
  tls_valid?: boolean;
  tls_expires_at?: string;
  tls_days_left?: number;
  health: Health;
}

export interface AlertInstance {
  id: string;
  alert_id: string;
  name: string;
  severity: Severity;
  state: "firing" | "resolved";
  server_id: string;
  started_at: string;
  resolved_at?: string;
  root_cause?: string;
  affected?: string[];
}

export interface EventItem {
  id: string;
  ts: string;
  severity: Severity;
  source: string;
  event: string;
  prev_state?: string;
  new_state?: string;
}

export interface MetricSeries {
  name: string;
  unit?: string;
  points: Array<{ t: number; v: number }>;
}

export interface ApiError {
  error: { code: string; message: string; request_id: string };
}

export interface MetricSample {
  timestamp: string;
  cpu_percent: number;
  load1: number;
  load5: number;
  load15: number;
  mem_total_bytes: number;
  mem_used_bytes: number;
  mem_avail_bytes: number;
  net_rx_bytes: number;
  net_tx_bytes: number;
}
