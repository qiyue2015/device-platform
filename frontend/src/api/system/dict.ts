import axios from 'axios';

// ---- Types ----

export interface DictTypeRecord {
  id: number;
  name: string;
  code: string;
  description: string | null;
  sort: number;
  is_active: boolean;
  created_at: string | null;
  updated_at: string | null;
}

export interface DictTypeListParams {
  id?: string;
  name?: string;
  code?: string;
  is_active?: string;
  sort?: string;
  current?: number;
  pageSize?: number;
}

export interface DictTypeCreateData {
  name: string;
  code: string;
  description?: string;
  sort?: number;
  is_active?: boolean;
}

export type DictTypeUpdateData = DictTypeCreateData;

export interface DictItemRecord {
  id: number;
  dictionary_type_id: number;
  label: string;
  value: string;
  sort: number;
  is_active: boolean;
  created_at: string | null;
  updated_at: string | null;
}

export interface DictItemListParams {
  id?: string;
  dictionary_type_id?: string;
  label?: string;
  value?: string;
  is_active?: string;
  sort?: string;
  current?: number;
  pageSize?: number;
}

export interface DictItemCreateData {
  dictionary_type_id: number;
  label: string;
  value: string;
  sort?: number;
  is_active?: boolean;
}

export interface DictItemUpdateData {
  label: string;
  value: string;
  sort?: number;
  is_active?: boolean;
}

// ---- Dict Type API ----

export function queryDictTypeList(params?: DictTypeListParams) {
  return axios.get<DictTypeRecord[]>('/api/system/dict-types', { params });
}

export function queryDictTypeDetail(id: number) {
  return axios.get<DictTypeRecord>(`/api/system/dict-types/${id}`);
}

export function queryDictItemsByCode(code: string) {
  return axios.get<DictItemRecord[]>(`/api/system/dict-types/${code}/items`);
}

export function createDictType(data: DictTypeCreateData) {
  return axios.post<DictTypeRecord>('/api/system/dict-types', data);
}

export function updateDictType(id: number, data: DictTypeUpdateData) {
  return axios.put<DictTypeRecord>(`/api/system/dict-types/${id}`, data);
}

export function deleteDictType(id: number) {
  return axios.delete(`/api/system/dict-types/${id}`);
}

// ---- Dict Item API ----

export function queryDictItemList(params?: DictItemListParams) {
  return axios.get<DictItemRecord[]>('/api/system/dict-items', { params });
}

export function queryDictItemDetail(id: number) {
  return axios.get<DictItemRecord>(`/api/system/dict-items/${id}`);
}

export function createDictItem(data: DictItemCreateData) {
  return axios.post<DictItemRecord>('/api/system/dict-items', data);
}

export function updateDictItem(itemId: number, data: DictItemUpdateData) {
  return axios.put<DictItemRecord>(`/api/system/dict-items/${itemId}`, data);
}

export function deleteDictItem(itemId: number) {
  return axios.delete(`/api/system/dict-items/${itemId}`);
}
