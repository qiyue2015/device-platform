import axios from 'axios';

// ---- Notification Types (real backend) ----

export interface NotificationRecord {
  id: string;
  type: string;
  title: string;
  content: string;
  read_at: string | null;
  created_at: string;
  data?: Record<string, unknown>;
}

export interface NotificationListRes {
  data: NotificationRecord[];
  total: number;
}

export function queryNotificationList(params?: { page?: number; page_size?: number }) {
  return axios.get<NotificationListRes>('/api/notifications', { params });
}

export function markNotificationRead(id: string) {
  return axios.patch(`/api/notifications/${id}/read`);
}

export function markAllNotificationsRead() {
  return axios.post('/api/notifications/read-all');
}

export function getUnreadCount() {
  return axios.get<{ count: number }>('/api/notifications/unread-count');
}

export function deleteNotification(id: string) {
  return axios.delete(`/api/notifications/${id}`);
}

export function clearAllNotifications() {
  return axios.delete('/api/notifications');
}

// ---- Legacy types (kept for backward compatibility) ----

export interface MessageRecord {
  id: number;
  type: string;
  title: string;
  subTitle: string;
  avatar?: string;
  content: string;
  time: string;
  status: 0 | 1;
  messageType?: number;
}
export type MessageListType = MessageRecord[];

/** @deprecated Use queryNotificationList instead */
export function queryMessageList() {
  return axios.post<MessageListType>('/api/message/list');
}

/** @deprecated Use markNotificationRead / markAllNotificationsRead instead */
export function setMessageStatus(data: { ids: number[] }) {
  return axios.post<MessageListType>('/api/message/read', data);
}

export interface ChatRecord {
  id: number;
  username: string;
  content: string;
  time: string;
  isCollect: boolean;
}

export function queryChatList() {
  return axios.post<ChatRecord[]>('/api/chat/list');
}
