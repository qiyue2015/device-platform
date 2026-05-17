type StatusTone = 'arcoblue' | 'green' | 'orange' | 'red' | 'purple' | 'gray';

export interface BusinessStatusMeta {
  label: string;
  color: StatusTone;
}

type StatusGroup = 'command' | 'webhook' | 'connection' | 'lifecycle';

const fallbackStatus: BusinessStatusMeta = {
  label: 'unknown',
  color: 'gray',
};

export const commandStatusMap: Record<string, BusinessStatusMeta> = {
  created: { label: 'created', color: 'gray' },
  queued: { label: 'queued', color: 'arcoblue' },
  sent: { label: 'sent', color: 'purple' },
  acked: { label: 'acked', color: 'arcoblue' },
  success: { label: 'success', color: 'green' },
  failed: { label: 'failed', color: 'red' },
  timeout: { label: 'timeout', color: 'red' },
  cancelled: { label: 'cancelled', color: 'gray' },
  offline: { label: 'offline', color: 'orange' },
};

export const webhookStatusMap: Record<string, BusinessStatusMeta> = {
  pending: { label: 'pending', color: 'orange' },
  sending: { label: 'sending', color: 'arcoblue' },
  delivered: { label: 'delivered', color: 'green' },
  failed: { label: 'failed', color: 'red' },
  dead: { label: 'dead', color: 'red' },
};

export const connectionStatusMap: Record<string, BusinessStatusMeta> = {
  unknown: { label: 'unknown', color: 'gray' },
  online: { label: 'online', color: 'green' },
  offline: { label: 'offline', color: 'orange' },
};

export const lifecycleStatusMap: Record<string, BusinessStatusMeta> = {
  active: { label: 'active', color: 'green' },
  disabled: { label: 'disabled', color: 'orange' },
  deleted: { label: 'deleted', color: 'red' },
};

const statusMaps: Record<StatusGroup, Record<string, BusinessStatusMeta>> = {
  command: commandStatusMap,
  webhook: webhookStatusMap,
  connection: connectionStatusMap,
  lifecycle: lifecycleStatusMap,
};

export function getBusinessStatusMeta(group: StatusGroup, status?: string): BusinessStatusMeta {
  if (!status) return fallbackStatus;
  return (
    statusMaps[group][status] || {
      label: status,
      color: fallbackStatus.color,
    }
  );
}
