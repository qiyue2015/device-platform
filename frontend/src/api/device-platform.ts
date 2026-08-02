import axios from 'axios';

export interface PaginationMeta {
  page: number;
  page_size: number;
  total: number;
}

interface ListEnvelope<T> {
  items: T[];
}

interface APIEnvelope<T> {
  data: T;
  meta?: PaginationMeta;
}

export interface PageParams {
  page?: number;
  page_size?: number;
}

export interface ProjectRecord {
  id: string;
  name: string;
  manager_user_id: string;
  manager: {
    id: string;
    email: string;
    display_name: string;
    status: 'active' | 'disabled';
  };
  webhook_url?: string | null;
  webhook_configured?: boolean;
  ip_whitelist?: string[];
  created_at: string;
  updated_at: string;
}

export interface ProjectCredentialRecord extends ProjectRecord {
  api_key?: string;
  webhook_secret?: string;
}

export interface CapabilityActionRecord {
  identifier: string;
  payload_schema: Record<string, unknown>;
  risk_level: string;
  delivery_policy: string;
  dispatch_deadline_ms: number;
  provider_request_timeout_ms: number;
  result_observation_timeout_ms: number;
  retry_allowed: boolean;
  delivery_policy_override_allowed: boolean;
}

export interface DeviceTypeRecord {
  code: string;
  revision: number;
  name: string;
  actions: CapabilityActionRecord[];
}

export interface CloudProviderRecord {
  code: string;
  name: string;
  access_type: string;
  transport_protocol: string;
  adapter: string;
  profiles: string[];
  integration_status: 'unconfigured' | 'configured_unverified' | 'verified';
}

export interface DeviceStateRecord {
  state: Record<string, unknown>;
  evidence_status: string;
  reported_at: string | null;
  observed_at: string;
}

export interface DeviceRecord {
  id: string;
  project_id: string;
  device_type_code: string;
  name: string;
  provider_code: string;
  provider_profile: string;
  provider_device_id: string;
  access_type: string;
  transport_protocol: string;
  adapter: string;
  connection_status: string;
  lifecycle_status: string;
  current_state: DeviceStateRecord | null;
  last_seen_at: string | null;
  created_at: string;
  updated_at: string;
}

export interface CommandRecord {
  id: string;
  project_id: string;
  device_id: string;
  provider_code: string;
  provider_profile: string;
  command_type: string;
  payload: Record<string, unknown>;
  device_type_revision: number;
  delivery_policy: string;
  status: string;
  reason_code: string | null;
  reason_detail: string | null;
  confirmation_level: string;
  evidence_status: string;
  idempotency_key: string;
  queued_at: string;
  dispatch_deadline_at: string;
  sent_at: string | null;
  result_deadline_at: string | null;
  finished_at: string | null;
  created_at: string;
  updated_at: string;
}

export interface CommandAttemptRecord {
  attempt_no: number;
  phase: string;
  provider_code: string;
  adapter: string;
  provider_request_key: string;
  outcome: string | null;
  confirmation_level: string;
  evidence_status: string;
  request_summary?: Record<string, unknown>;
  response_summary?: Record<string, unknown>;
  reason_code: string | null;
  error_detail: string | null;
  claimed_at: string;
  dispatching_at: string | null;
  completed_at: string | null;
}

export interface CommandResultRecord {
  result_id: string;
  attempt_id: string | null;
  source: string;
  outcome: string;
  confirmation_level: string;
  evidence_status: string;
  reported_at: string | null;
  observed_at: string;
  late: boolean;
}

export interface EventRecord {
  event_id: string;
  schema_version: number;
  event_type: string;
  project_id: string;
  device_id: string | null;
  command_id: string | null;
  occurred_at: string;
  source: string;
  payload: Record<string, unknown>;
}

export interface CommandDetail extends CommandRecord {
  attempts: CommandAttemptRecord[];
  results: CommandResultRecord[];
  events: EventRecord[];
}

export interface WebhookDeliveryAttemptRecord {
  attempt_no: number;
  started_at: string;
  completed_at: string | null;
  http_status: number | null;
  response_summary: string | null;
  error_code: string | null;
  error_detail: string | null;
}

export interface WebhookDeliveryRecord {
  id: string;
  event_id: string;
  project_id: string;
  target_url: string;
  webhook_config_version: number;
  status: string;
  attempt_count: number;
  next_attempt_at: string | null;
  replay_of_delivery_id: string | null;
  delivered_at: string | null;
  created_at: string;
  updated_at: string;
  attempts?: WebhookDeliveryAttemptRecord[];
}

export interface AuditLogRecord {
  id: string;
  actor_type: string;
  actor_user_id: string | null;
  actor_id: string | null;
  project_id: string | null;
  action: string;
  result: string;
  resource_type: string;
  resource_id: string | null;
  ip_address: string | null;
  request_id: string | null;
  metadata: Record<string, unknown>;
  occurred_at: string;
}

export interface SimulatorState {
  outcome: string;
  delay_ms: number;
  version: number;
  updated_at: string;
}

async function queryPage<T>(url: string, filters: object = {}): Promise<APIEnvelope<T[]>> {
  const response = (await axios.get<ListEnvelope<T>>(url, { params: filters })) as unknown as APIEnvelope<ListEnvelope<T>>;

  return { ...response, data: response.data.items || [] };
}

export function queryProjects(filters: { name?: string; manager_user_id?: string } & PageParams = {}) {
  return queryPage<ProjectRecord>('/v1/projects', filters);
}

export function createProject(data: { name: string; manager_user_id: string; webhook_url?: string; ip_whitelist?: string[] }) {
  return axios.post<ProjectCredentialRecord>('/v1/projects', data);
}

export function updateProject(id: string, data: { name?: string; webhook_url?: string | null; ip_whitelist?: string[] }) {
  return axios.patch<ProjectCredentialRecord>(`/v1/projects/${id}`, data);
}

export function rotateProjectAPIKey(id: string) {
  return axios.post<ProjectCredentialRecord>(`/v1/projects/${id}/api-key/rotate`);
}

export function rotateProjectWebhookSecret(id: string) {
  return axios.post<ProjectCredentialRecord>(`/v1/projects/${id}/webhook-secret/rotate`);
}

export function transferProject(id: string, managerUserId: string) {
  return axios.post<ProjectRecord>(`/v1/projects/${id}/transfer`, { manager_user_id: managerUserId });
}

export function queryDeviceTypes(filters: PageParams = {}) {
  return queryPage<DeviceTypeRecord>('/v1/device-types', filters);
}

export function queryCloudProviders(filters: PageParams = {}) {
  return queryPage<CloudProviderRecord>('/v1/cloud-providers', filters);
}

export function queryDevices(
  filters: {
    project_id?: string;
    device_type_code?: string;
    provider_code?: string;
    connection_status?: string;
    lifecycle_status?: string;
  } & PageParams = {}
) {
  return queryPage<DeviceRecord>('/v1/devices', filters);
}

export function createDevice(data: {
  project_id: string;
  name: string;
  device_type_code: string;
  provider_code: string;
  provider_profile: string;
  provider_device_id?: string;
}) {
  return axios.post<DeviceRecord>('/v1/devices', data);
}

export function updateDevice(id: string, data: { name?: string; lifecycle_status?: string }) {
  return axios.patch<DeviceRecord>(`/v1/devices/${id}`, data);
}

export function queryCommands(
  filters: {
    project_id?: string;
    device_id?: string;
    command_type?: string;
    status?: string;
  } & PageParams = {}
) {
  return queryPage<CommandRecord>('/v1/device-commands', filters);
}

export function createCommand(data: {
  project_id: string;
  device_id: string;
  command_type: string;
  payload?: Record<string, unknown>;
  idempotency_key: string;
}) {
  return axios.post<CommandRecord>('/v1/device-commands', {
    project_id: data.project_id,
    device_id: data.device_id,
    command_type: data.command_type,
    payload: data.payload || {},
    idempotency_key: data.idempotency_key,
  });
}

export function queryCommandDetail(id: string) {
  return axios.get<CommandDetail>(`/v1/device-commands/${id}`);
}

export function cancelCommand(id: string) {
  return axios.post<CommandRecord>(`/v1/device-commands/${id}/cancel`);
}

export function queryEvents(
  filters: {
    project_id?: string;
    device_id?: string;
    command_id?: string;
    event_type?: string;
  } & PageParams = {}
) {
  return queryPage<EventRecord>('/v1/events', filters);
}

export function queryEventDetail(id: string) {
  return axios.get<EventRecord>(`/v1/events/${id}`);
}

export function queryWebhookDeliveries(filters: { project_id?: string; event_id?: string; status?: string } & PageParams = {}) {
  return queryPage<WebhookDeliveryRecord>('/v1/webhook-deliveries', filters);
}

export function queryWebhookDeliveryDetail(id: string) {
  return axios.get<WebhookDeliveryRecord>(`/v1/webhook-deliveries/${id}`);
}

export function resendWebhookDelivery(id: string) {
  return axios.post<WebhookDeliveryRecord>(`/v1/webhook-deliveries/${id}/resend`);
}

export function queryAuditLogs(
  filters: {
    project_id?: string;
    actor_type?: string;
    action?: string;
    result?: string;
    resource_type?: string;
    resource_id?: string;
  } & PageParams = {}
) {
  return queryPage<AuditLogRecord>('/v1/audit-logs', filters);
}

export function getSimulator() {
  return axios.get<SimulatorState>('/v1/simulator');
}

export function updateSimulator(data: { outcome: string; delay_ms: number }) {
  return axios.patch<SimulatorState>('/v1/simulator', data);
}
