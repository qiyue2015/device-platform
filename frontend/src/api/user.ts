import axios from 'axios';
import type { RouteRecordNormalized } from 'vue-router';
import { UserState } from '@/store/modules/user/types';

export interface LoginData {
  email: string;
  password: string;
}

export interface LoginRes {
  access_token: string;
  token_type: string;
  expires_in: string;
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
  return axios.get<UserState>('/v1/auth/me');
}

export function getMenuList() {
  return axios.get<RouteRecordNormalized[]>('/v1/auth/menu');
}
