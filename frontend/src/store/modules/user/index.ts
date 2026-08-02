import { defineStore } from 'pinia';
import { login as userLogin, logout as userLogout, getUserInfo, LoginData } from '@/api/user';
import { setToken, clearToken } from '@/utils/auth';
import { removeRouteListener } from '@/utils/route-listener';
import { UserState } from './types';

const useUserStore = defineStore('user', {
  state: (): UserState => ({
    id: '',
    email: undefined,
    display_name: undefined,
    is_super_admin: false,
    status: undefined,
    created_at: undefined,
    updated_at: undefined,
    roles: [],
  }),

  getters: {
    userInfo(state: UserState): UserState {
      return { ...state };
    },
  },

  actions: {
    setInfo(partial: Partial<UserState>) {
      this.$patch(partial);
    },
    resetInfo() {
      this.$reset();
    },
    async info() {
      const res = await getUserInfo();
      this.setInfo({
        ...res.data,
        roles: [res.data.is_super_admin ? 'super-admin' : 'user'],
      });
    },
    async login(loginForm: LoginData) {
      try {
        const res = await userLogin(loginForm);
        setToken(res.data.access_token);
      } catch (err) {
        clearToken();
        throw err;
      }
    },
    logoutCallBack() {
      this.resetInfo();
      clearToken();
      removeRouteListener();
    },
    async logout() {
      try {
        await userLogout();
        this.logoutCallBack();
      } catch {
        this.logoutCallBack();
      }
    },
  },
});

export default useUserStore;
