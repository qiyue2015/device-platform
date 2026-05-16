export type RoleType = '' | '*' | 'super-admin' | 'admin' | 'user';
export interface UserState {
  id: string;
  name?: string;
  nickname?: string;
  email?: string;
  email_verified: boolean;
  roles: RoleType[];
}
