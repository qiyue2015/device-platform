import axios from 'axios';

// ---- Types ----

export interface MenuRecord {
  id: number;
  parent_id: number;
  type: number;
  name: string;
  path: string;
  component: string | null;
  permission: string | null;
  locale: string;
  icon: string | null;
  sort: number;
  is_hidden: boolean;
  is_active: boolean;
  created_at: string;
  updated_at: string;
  children: MenuRecord[];
}

export interface MenuCreateData {
  parent_id?: number;
  type?: number;
  name: string;
  path?: string;
  component?: string;
  permission?: string;
  locale: string;
  icon?: string;
  sort?: number;
  is_hidden?: boolean;
  is_active?: boolean;
}

export type MenuUpdateData = MenuCreateData;

// ---- API ----

export function queryMenuTree() {
  return axios.get<MenuRecord[]>('/api/system/menus');
}

export function queryMenuDetail(menuId: number) {
  return axios.get<MenuRecord>(`/api/system/menus/${menuId}`);
}

export function createMenu(data: MenuCreateData) {
  return axios.post<MenuRecord>('/api/system/menus', data);
}

export function updateMenu(menuId: number, data: MenuUpdateData) {
  return axios.put<MenuRecord>(`/api/system/menus/${menuId}`, data);
}

export function deleteMenu(menuId: number) {
  return axios.delete(`/api/system/menus/${menuId}`);
}
