// Frontend view of the API types. Mirrors packages/types and docs/API.md.

export type Health = "HEALTHY" | "DEGRADED" | "DOWN" | "UNKNOWN";
export type Severity = "INFO" | "WARNING" | "CRITICAL";
export type Role = "owner" | "admin" | "viewer";

export interface SessionInfo {
  email: string;
  role: Role;
  permissions: string[];
}

export interface Server {
  id: string;
  org_id: string;
  server_id: string;
  hostname: string;
  mode: string;
  status: Health;
  last_seen_at?: string;
  created_at?: string;
}

export interface SummaryCounts {
  services_healthy: number;
  services_degraded: number;
  services_down: number;
  containers_running: number;
  containers_unhealthy: number;
  domains_online: number;
  domains_ssl_expiring: number;
  alerts_critical: number;
  alerts_warning: number;
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
  counts: SummaryCounts;
}

export interface Resource {
  type: string;
  id: string;
  name: string;
  status?: string;
  health?: Health;
  labels?: Record<string, string>;
  attributes?: Record<string, unknown>;
  ports?: { host?: number; container?: number; protocol: string }[];
  networks?: string[];
  volumes?: string[];
  depends_on?: string[];
  detected_by: string;
}

export interface DomainView {
  fqdn: string;
  tls: boolean;
  tls_days_left?: number;
  tls_expires_at?: string;
  health: Health;
  source: string;
}

export interface SecurityFinding {
  severity: Severity;
  title: string;
  detail: string;
  recommendation: string;
}

export interface AlertInstance {
  id: string;
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

export interface MetricPoint {
  t: number;
  v: number;
}
export interface MetricSeries {
  name: string;
  unit?: string;
  points: MetricPoint[];
}
export interface MetricsResponse {
  series: MetricSeries[];
  degraded?: boolean;
  note?: string;
}

export interface Topology {
  nodes: { id: string; label: string; type: string; health?: Health }[];
  edges: { from: string; to: string; source: string }[];
}

export interface DetectorResult {
  id: string;
  name: string;
  version: string;
  available: boolean;
  reason?: string;
  count: number;
  duration_ms: number;
  error?: string;
}

export interface Page<T> {
  data: T[];
  next_cursor?: string;
}
