import axios from 'axios';

export interface ProjectRecord {
  id: string;
  code: string;
  name: string;
  api_key?: string;
  webhook_url: string;
  webhook_secret?: string;
  ip_whitelist: string[];
  created_at: string;
  updated_at: string;
}

export interface DeviceRecord {
  id: string;
  project_id: string;
  device_type: string;
  name: string;
  lifecycle_status: string;
  connection_status: string;
  provider_code: string;
  provider_device_id: string;
  access_type: string;
  transport_protocol: string;
  adapter: string;
  capabilities: string[];
  current_state: Record<string, unknown>;
  created_at: string;
  updated_at: string;
}

export interface CloudProviderRecord {
  code: string;
  name: string;
  access_type: string;
  transport_protocol: string;
  adapter: string;
  configured: boolean;
}

export interface CommandRecord {
  id: string;
  project_id: string;
  device_id: string;
  command_type: string;
  payload: Record<string, unknown>;
  idempotency_key?: string;
  delivery_policy: string;
  status: string;
  failure_reason?: string;
  cancel_reason?: string;
  corrected?: boolean;
  created_at: string;
  updated_at: string;
}

export interface WebhookDeliveryRecord {
  id: string;
  event_id: string;
  project_id: string;
  device_id: string;
  webhook_url: string;
  status: string;
  attempt_count: number;
  max_attempts: number;
  last_error?: string;
  created_at: string;
  delivered_at?: string;
}

export interface AuditLogRecord {
  id: string;
  action: string;
  actor_id: string;
  project_id?: string;
  device_id?: string;
  ip: string;
  summary: Record<string, unknown>;
  created_at: string;
}

export interface SimulatorState {
  mode: string;
  delay_ms: number;
  heartbeat_active: boolean;
  updated_at: string;
}

export interface CommandDetail {
  command: CommandRecord;
  attempts: Array<Record<string, unknown>>;
  events: Array<Record<string, unknown>>;
}

interface ListEnvelope<T> {
  items: T[];
}

function normalizeList<T>(value: T[] | ListEnvelope<T>): T[] {
  if (Array.isArray(value)) {
    return value;
  }
  return value.items || [];
}

export function queryProjects() {
  return axios.get<ProjectRecord[]>('/v1/projects');
}

export function createProject(data: Partial<ProjectRecord>) {
  return axios.post<ProjectRecord>('/v1/projects', data);
}

export function updateProject(id: string, data: Partial<ProjectRecord>) {
  return axios.patch<ProjectRecord>(`/v1/projects/${id}`, data);
}

export function queryCloudProviders() {
  return axios.get<CloudProviderRecord[]>('/v1/cloud-providers');
}

export function queryDevices(projectId?: string) {
  return axios.get<DeviceRecord[]>('/v1/devices', {
    params: projectId ? { project_id: projectId } : undefined,
  });
}

export function createDevice(data: Partial<DeviceRecord>) {
  return axios.post<DeviceRecord>('/v1/devices', data);
}

export function queryCommands(projectId?: string) {
  return axios.get<CommandRecord[]>('/v1/device-commands', {
    params: projectId ? { project_id: projectId } : undefined,
  });
}

export function createCommand(data: {
  project_id?: string;
  device_id: string;
  command_type: string;
  payload?: Record<string, unknown>;
  idempotency_key?: string;
  delivery_policy?: string;
}) {
  return axios.post<CommandRecord>(
    '/v1/device-commands',
    {
      device_id: data.device_id,
      command_type: data.command_type,
      payload: data.payload || {},
      idempotency_key: data.idempotency_key,
      delivery_policy: data.delivery_policy,
    },
    {
      headers: data.project_id ? { 'X-Project-ID': data.project_id } : undefined,
    }
  );
}

export function queryCommandDetail(id: string, projectId?: string) {
  return axios.get<CommandDetail>(`/v1/device-commands/${id}`, {
    params: projectId ? { project_id: projectId } : undefined,
  });
}

export function queryWebhookDeliveries() {
  return axios.get<WebhookDeliveryRecord[] | ListEnvelope<WebhookDeliveryRecord>>('/v1/webhook-deliveries').then((res) => ({
    ...res,
    data: normalizeList(res.data),
  }));
}

export function resendWebhookDelivery(id: string) {
  return axios.post<WebhookDeliveryRecord>(`/v1/webhook-deliveries/${id}/resend`);
}

export function queryAuditLogs() {
  return axios.get<AuditLogRecord[] | ListEnvelope<AuditLogRecord>>('/v1/audit-logs').then((res) => ({
    ...res,
    data: normalizeList(res.data),
  }));
}

export function getSimulator() {
  return axios.get<SimulatorState>('/v1/simulator');
}

export function updateSimulator(data: { mode: string; delay_ms?: number }) {
  return axios.patch<SimulatorState>('/v1/simulator', data);
}
