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

export interface LogoutRes {
  logout_url: string;
}

export function login(data: LoginData) {
  return axios.post<LoginRes>('/v1/auth/login', data);
}

export function refreshToken() {
  return axios.post<LoginRes>('/v1/auth/refresh');
}

export function exchangeToken(code: string) {
  return axios.post<LoginRes>('/api/auth/exchange', { code });
}

export function logout() {
  return axios.post<LogoutRes>('/v1/auth/logout');
}

export function getUserInfo() {
  return axios.get<UserState>('/v1/auth/me');
}

export function getMenuList() {
  return axios.get<RouteRecordNormalized[]>('/v1/auth/menu');
}

// 更新当前用户资料
export function updateProfile(data: { name?: string; nickname?: string; introduction?: string }) {
  return axios.patch('/api/me', data);
}

// 更新当前用户头像
export interface UpdateAvatarRes {
  avatar_url: string;
}

export function updateAvatar(file: File) {
  const formData = new FormData();
  formData.append('avatar', file);
  return axios.post<UpdateAvatarRes>('/api/me/avatar', formData, {
    headers: {
      'Content-Type': 'multipart/form-data',
    },
  });
}

// 修改密码
export function changePassword(data: { password: string; new_password: string; password_confirmation: string }) {
  return axios.patch('/api/me/password', data);
}

// 修改邮箱
export function updateEmail(data: { email: string; code: string }) {
  return axios.patch('/api/me/email', data);
}

// 修改手机号
export function updatePhone(data: { phone: string; code: string }) {
  return axios.patch('/api/me/phone', data);
}

// 发送密码重置邮件
export function forgotPassword(data: { email: string }) {
  return axios.post('/api/password/forgot', data);
}

// 密码重置（忘记密码流程）
export function resetPassword(data: { token: string; email: string; password: string; password_confirmation: string }) {
  return axios.post('/api/password/reset', data);
}

export interface RegisterData {
  phone: string;
  code: string;
  password: string;
  invite_code?: string;
}

export function register(data: RegisterData) {
  return axios.post('/api/user/register', data);
}
