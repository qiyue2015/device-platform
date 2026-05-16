import axios from 'axios';

// ---- Types ----

export interface LogCauser {
  id: number;
  name: string;
  email: string;
}

export interface LogRecord {
  id: number;
  log_name: string | null;
  description: string;
  subject_type: string | null;
  event: string | null;
  subject_id: number | null;
  causer_type: string | null;
  causer_id: number | null;
  properties: Record<string, unknown> | null;
  batch_uuid: string | null;
  created_at: string | null;
  updated_at: string | null;
  causer: LogCauser | null;
  subject: LogCauser | null;
}

export interface LogListParams {
  'id'?: string;
  'log_name'?: string[];
  'event'?: string;
  'causer_id'?: string;
  'description'?: string;
  'date_from:created_at'?: string;
  'date_to:created_at'?: string;
  'sort'?: string;
  'current'?: number;
  'pageSize'?: number;
}

// ---- API ----

export function queryLogList(params?: LogListParams) {
  return axios.get<LogRecord[]>('/api/system/audit-logs', {
    params,
    paramsSerializer: (p) => {
      const parts: string[] = [];
      Object.entries(p).forEach(([key, val]) => {
        if (val == null) return;
        if (Array.isArray(val)) {
          val.forEach((v) => parts.push(`${encodeURIComponent(`${key}[]`)}=${encodeURIComponent(v)}`));
        } else {
          parts.push(`${encodeURIComponent(key)}=${encodeURIComponent(val as string)}`);
        }
      });
      return parts.join('&');
    },
  });
}

export function queryLogDetail(id: number) {
  return axios.get<LogRecord>(`/api/system/audit-logs/${id}`);
}
