// Typed API client. Uses cookie-based sessions (credentials: "include"). Never
// stores tokens in localStorage. Surfaces structured API errors.

import type {
  Server,
  ServerSummary,
  SessionInfo,
  Resource,
  DomainView,
  SecurityAudit,
  Alert,
  AlertInstance,
  EventItem,
  MetricsResponse,
  LogsResponse,
  Topology,
  DetectorResult,
  Page,
} from "./types";

export class ApiError extends Error {
  code: string;
  requestId: string;
  status: number;
  constructor(status: number, code: string, message: string, requestId = "") {
    super(message);
    this.status = status;
    this.code = code;
    this.requestId = requestId;
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const resp = await fetch(`/api/v1${path}`, {
    ...init,
    credentials: "include",
    headers: {
      "Content-Type": "application/json",
      ...(init?.headers ?? {}),
    },
  });
  const text = await resp.text();
  const body = text ? JSON.parse(text) : {};
  if (!resp.ok) {
    const e = body?.error ?? {};
    throw new ApiError(resp.status, e.code ?? "INTERNAL_ERROR", e.message ?? resp.statusText, e.request_id);
  }
  return body as T;
}

export const api = {
  // auth
  login: (email: string, password: string) =>
    request<SessionInfo>("/auth/login", { method: "POST", body: JSON.stringify({ email, password }) }),
  logout: () => request<{ status: string }>("/auth/logout", { method: "POST" }),
  session: () => request<SessionInfo>("/auth/session"),
  authProviders: () => request<{ providers: { name: string; start: string }[] }>("/auth/providers"),

  // servers
  servers: () => request<Page<Server>>("/servers"),
  server: (id: string) => request<Server>(`/servers/${id}`),
  summary: (id: string) => request<ServerSummary>(`/servers/${id}/summary`),
  deleteServer: (id: string) => request<{ status: string }>(`/servers/${id}`, { method: "DELETE" }),

  // discovery-derived
  discovery: (id: string) =>
    request<{ resources: Resource[]; detectors: DetectorResult[]; topology?: Topology }>(`/servers/${id}/discovery`),
  containers: (id: string) => request<Page<Resource>>(`/servers/${id}/containers`),
  services: (id: string) => request<Page<Resource>>(`/servers/${id}/services`),
  databases: (id: string) => request<Page<Resource>>(`/servers/${id}/databases`),
  applications: (id: string) => request<Page<Resource>>(`/servers/${id}/applications`),
  domains: (id: string) => request<Page<DomainView>>(`/servers/${id}/domains`),
  security: (id: string, check?: string) =>
    request<SecurityAudit>(`/servers/${id}/security${check ? `?check=${encodeURIComponent(check)}` : ""}`),
  topology: (id: string) => request<Topology>(`/servers/${id}/topology`),
  metrics: (id: string, query: string, range: string) =>
    request<MetricsResponse>(`/servers/${id}/metrics?query=${encodeURIComponent(query)}&range=${range}`),
  logs: (id: string, source?: string, q?: string) => {
    const params = new URLSearchParams();
    if (source) params.set("source", source);
    if (q) params.set("q", q);
    const qs = params.toString();
    return request<LogsResponse>(`/servers/${id}/logs${qs ? `?${qs}` : ""}`);
  },

  // alerts / events
  alerts: () => request<Page<Alert>>("/alerts"),
  createAlert: (body: Partial<Alert>) => request<Alert>("/alerts", { method: "POST", body: JSON.stringify(body) }),
  updateAlert: (id: string, body: Partial<Alert>) => request<Alert>(`/alerts/${id}`, { method: "PATCH", body: JSON.stringify(body) }),
  deleteAlert: (id: string) => request<{ status: string }>(`/alerts/${id}`, { method: "DELETE" }),
  alertInstances: () => request<Page<AlertInstance>>("/alerts/instances"),
  events: (limit = 50) => request<Page<EventItem>>(`/events?limit=${limit}`),

  // enrollment (cloud)
  createEnrollmentToken: () =>
    request<{ enrollment_token: string; expires_at: string }>("/agents/enrollment-tokens", { method: "POST" }),
};
