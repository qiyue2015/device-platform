import type { UserStatus } from '@/api/user';

export type RoleType = '' | '*' | 'super-admin' | 'user';
export interface UserState {
  id: string;
  email?: string;
  display_name?: string;
  is_super_admin: boolean;
  status?: UserStatus;
  created_at?: string;
  updated_at?: string;
  roles: RoleType[];
}
