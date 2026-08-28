// Frontend view of the API types. Mirrors packages/types and docs/API.md.

export type Health = "HEALTHY" | "DEGRADED" | "DOWN" | "UNKNOWN";
export type Severity = "INFO" | "WARNING" | "CRITICAL";
export type Role = "owner" | "admin" | "viewer";

export interface SessionInfo {
  email: string;
  role: Role;
  permissions: string[];
  /**
   * False for an identity-provider account that has never had a Pulse
   * password — the normal case on Pulse Cloud, where people sign in with
   * Google or Telegram. Password and email management does not apply to those
   * accounts and is hidden rather than shown broken.
   */
  has_password: boolean;
}

/** First-boot provisioning state reported by GET /setup/status. */
export interface SetupStatus {
  needs_setup: boolean;
  mode: string;
  self_hosted: boolean;
  min_password_length: number;
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
  url: string;
  tls: boolean;
  ssl: boolean;
  tls_days_left?: number;
  tls_expires_at?: string;
  upstream?: string;
  ports?: string[];
  health: Health;
  source: string;
}

// --- Unified inventory ---
// The correlated host-vs-container view of everything running on a server:
// containers, host services, databases and reverse proxies, each with the
// listening sockets attributed to it.

export interface InventoryPort {
  port: number;
  protocol: string;
  address?: string;
  exposure?: string;
}

export type InventoryKind = "container" | "service" | "database" | "proxy";
export type InventoryPlacement = "host" | "container";

export interface InventoryItem {
  id: string;
  name: string;
  kind: InventoryKind;
  placement: InventoryPlacement;
  status?: string;
  health?: Health;
  engine?: string;
  image?: string;
  unit?: string;
  pid?: number;
  container_id?: string;
  ports?: InventoryPort[];
  detected_by?: string;
}

export interface InventoryTotals {
  host_workloads: number;
  container_workloads: number;
  listening_ports: number;
  public_ports: number;
  databases: number;
  unattributed_ports: number;
}

export interface InventoryResponse {
  hostname: string;
  generated_at: string;
  totals: InventoryTotals;
  items: InventoryItem[];
  /** Listening sockets whose owning process could not be read. */
  unattributed: InventoryPort[];
}

// --- Service audit ---
// Relationships between services, plus advisory findings about what nothing
// appears to need. Every finding carries its evidence and a confidence, and the
// response states its own blind spots — the analysis is a lead, not a verdict,
// and Pulse never acts on it.

export type AuditSeverity = "info" | "low" | "medium";
export type AuditConfidence = "low" | "medium" | "high";
export type AuditCategory = "stopped" | "unrouted" | "idle" | "unreferenced" | "duplicate" | "orphaned";
export type RelationKind = "proxy_route" | "docker_network" | "compose" | "depends_on" | "port";

export interface ServiceUsage {
  cpu_percent: number;
  memory_bytes: number;
  disk_bytes: number;
  net_rx_bytes: number;
  net_tx_bytes: number;
}

export interface ServiceRelation {
  from: string;
  to: string;
  kind: RelationKind;
  detail?: string;
}

export interface ServiceNode {
  id: string;
  name: string;
  kind: InventoryKind;
  placement: InventoryPlacement;
  status?: string;
  health?: Health;
  image?: string;
  engine?: string;
  project?: string;
  ports?: number[];
  public_ports?: number[];
  usage: ServiceUsage;
  inbound_routes?: string[];
  peers?: string[];
  /** Infrastructure that is exempt from every waste rule. */
  essential: boolean;
}

export interface AuditFinding {
  id: string;
  subject: string;
  subject_name: string;
  category: AuditCategory;
  severity: AuditSeverity;
  confidence: AuditConfidence;
  title: string;
  detail: string;
  evidence: string[];
  reclaimable: ServiceUsage;
  recommendation: string;
}

export interface AuditTotals {
  services: number;
  relations: number;
  flagged: number;
  reclaimable_memory_bytes: number;
  reclaimable_disk_bytes: number;
  unrouted_services: number;
  stopped_with_disk: number;
}

export interface ServiceAuditResponse {
  hostname: string;
  generated_at: string;
  nodes: ServiceNode[];
  relations: ServiceRelation[];
  findings: AuditFinding[];
  totals: AuditTotals;
  /** What this analysis cannot see. Read before acting on any finding. */
  limitations: string[];
}

export type FindingSeverity = "CRITICAL" | "HIGH" | "MEDIUM" | "LOW" | "INFO";
export type CheckStatus = "pass" | "issues" | "error" | "skipped" | "not_run";
export type CheckKind = "passive" | "active";
export type ScanStatus = "queued" | "running" | "done" | "error";
export type ScanMode = "passive" | "active" | "full";
export type LogLevel = "info" | "warn" | "error" | "success";

export interface SecurityCategory {
  id: string;
  name: string;
  description: string;
  icon: string;
}
export interface SecurityFinding {
  id: string;
  check_id: string;
  category: string;
  severity: FindingSeverity;
  title: string;
  resource?: string;
  detail: string;
  evidence?: string;
  recommendation: string;
  cvss?: number;
  owasp?: string;
  cwe?: string;
  references?: string[];
}
export interface SecurityCheck {
  id: string;
  category: string;
  name: string;
  description: string;
  kind: CheckKind;
  owasp?: string;
  status: CheckStatus;
  count: number;
  note?: string;
  duration_ms?: number;
}
export interface ScanLog {
  t: string;
  level: LogLevel;
  check?: string;
  msg: string;
}
export interface ScanState {
  id: string;
  server_id: string;
  status: ScanStatus;
  mode: ScanMode;
  categories?: string[];
  targets?: string[];
  progress: number;
  current?: string;
  total: number;
  completed: number;
  started_at: string;
  finished_at?: string;
  checks: SecurityCheck[];
  findings: SecurityFinding[];
  logs: ScanLog[];
  error?: string;
}
export interface SecurityAudit {
  categories: SecurityCategory[];
  checks: SecurityCheck[];
  latest?: ScanState | null;
}

export interface Alert {
  id: string;
  org_id: string;
  name: string;
  expr: string;
  severity: Severity;
  for_seconds: number;
  cooldown_seconds: number;
  enabled: boolean;
}

export interface AlertInstance {
  id: string;
  name: string;
  severity: Severity;
  state: "firing" | "resolved";
  server_id: string;
  dedup_key: string;
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

export interface LogEntry {
  source: string;
  stream: string;
  time: string;
  message: string;
}
export interface LogsResponse {
  sources: string[];
  entries: LogEntry[];
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

// --- SSH console ---
// The console is the only Pulse feature that can change a server. It is off
// unless the operator enables it on the API, and viewers never get access.

export interface SSHCapabilities {
  enabled: boolean;
  reason: string;
  default_port: number;
  can_use: boolean;
}

export type SSHAuthMethod = "password" | "key";

export interface SSHOpenRequest {
  host: string;
  port: number;
  username: string;
  auth_method: SSHAuthMethod;
  password?: string;
  private_key?: string;
  passphrase?: string;
  known_fingerprint?: string;
  cols: number;
  rows: number;
}

export interface SSHSession {
  session_id: string;
  host: string;
  port: number;
  username: string;
  fingerprint: string;
  first_connection: boolean;
  attach_within_sec: number;
}

/** One thing the SSH setup routine did, skipped or found on the host. */
export interface SSHSetupStep {
  name: string;
  status: "ok" | "skipped" | "warn" | "error";
  detail: string;
}

export interface SSHSetupResult {
  steps: SSHSetupStep[];
  info: Record<string, string>;
  public_key: string;
  /** OpenSSH private key, returned once. The control plane never stores it. */
  private_key: string;
  fingerprint: string;
  verified: boolean;
  warnings?: string[];
}
