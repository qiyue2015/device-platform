import axios from 'axios';

export interface LoginData {
  email: string;
  password: string;
}

export interface LoginRes {
  access_token: string;
  token_type: string;
  expires_in: string;
}

export type UserStatus = 'active' | 'disabled';

export interface UserRecord {
  id: string;
  email: string;
  display_name: string;
  is_super_admin: boolean;
  status: UserStatus;
  created_at: string;
  updated_at: string;
}

export interface UserPageParams {
  email?: string;
  status?: UserStatus;
  page?: number;
  page_size?: number;
}

interface UserListEnvelope {
  items: UserRecord[];
}

interface UserListResponse {
  data: UserListEnvelope;
  meta?: {
    page: number;
    page_size: number;
    total: number;
  };
}

export function login(data: LoginData) {
  return axios.post<LoginRes>('/v1/auth/login', data);
}

export function refreshToken() {
  return axios.post<LoginRes>('/v1/auth/refresh');
}

export function logout() {
  return axios.post('/v1/auth/logout');
}

export function getUserInfo() {
  return axios.get<UserRecord>('/v1/auth/me');
}

export async function queryUsers(filters: UserPageParams = {}) {
  const response = (await axios.get<UserListEnvelope>('/v1/users', { params: filters })) as unknown as UserListResponse;
  return { ...response, data: response.data.items || [] };
}

export function createUser(data: { email: string; display_name: string; password: string }) {
  return axios.post<UserRecord>('/v1/users', data);
}

export function updateUserStatus(id: string, status: UserStatus) {
  return axios.patch<UserRecord>(`/v1/users/${id}`, { status });
}
