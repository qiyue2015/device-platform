import axios from 'axios';

// ---- Types ----

export interface MenuPivot {
  role_id: number;
  menu_id: number;
}

export interface RoleMenu {
  id: number;
  name: string;
  pivot: MenuPivot;
}

export interface RoleRecord {
  id: number;
  name: string;
  locale: string | null;
  guard_name: string;
  created_at: string | null;
  updated_at: string | null;
  menus?: RoleMenu[];
}

export interface RoleListParams {
  id?: string;
  name?: string;
  sort?: string;
  current?: number;
  pageSize?: number;
}

export interface RoleCreateData {
  name: string;
  locale?: string;
  menu_ids: number[];
}

export interface RoleUpdateData {
  name: string;
  locale?: string;
  menu_ids: number[];
}

// ---- API ----

export function queryRoleList(params?: RoleListParams) {
  return axios.get<RoleRecord[]>('/api/system/roles', { params });
}

export function queryRoleDetail(roleId: number) {
  return axios.get<RoleRecord>(`/api/system/roles/${roleId}`);
}

export function createRole(data: RoleCreateData) {
  return axios.post<RoleRecord>('/api/system/roles', data);
}

export function updateRole(roleId: number, data: RoleUpdateData) {
  return axios.put<RoleRecord>(`/api/system/roles/${roleId}`, data);
}

export function deleteRole(roleId: number) {
  return axios.delete(`/api/system/roles/${roleId}`);
}
