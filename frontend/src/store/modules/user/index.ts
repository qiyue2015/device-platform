import { defineStore } from 'pinia';
import {
  login as userLogin,
  logout as userLogout,
  register as userRegister,
  exchangeToken as apiExchangeToken,
  getUserInfo,
  updateAvatar as apiUpdateAvatar,
  LoginData,
  RegisterData,
} from '@/api/user';
import { setToken, clearToken } from '@/utils/auth';
import { isOidc } from '@/utils/auth-strategy';
import { removeRouteListener } from '@/utils/route-listener';
import { UserState } from './types';
import useAppStore from '../app';

const useUserStore = defineStore('user', {
  state: (): UserState => ({
    id: '',
    name: undefined,
    nickname: undefined,
    avatar: undefined,
    email: undefined,
    email_verified: false,
    mobile: '',
    mobile_verified: false,
    introduction: undefined,
    roles: [],
  }),

  getters: {
    userInfo(state: UserState): UserState {
      return { ...state };
    },
  },

  actions: {
    switchRoles() {
      return new Promise((resolve) => {
        this.roles = this.roles.includes('user') ? ['admin'] : ['user'];
        resolve(this.roles);
      });
    },
    setInfo(partial: Partial<UserState>) {
      this.$patch(partial);
    },
    resetInfo() {
      this.$reset();
    },
    async info() {
      const res = await getUserInfo();
      this.setInfo(res.data);
    },
    async updateAvatar(file: File) {
      const res = await apiUpdateAvatar(file);
      // Update avatar in store
      this.setInfo({ avatar: res.data.avatar_url });
      return res;
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
    async register(registerForm: RegisterData) {
      try {
        const res = await userRegister(registerForm);
        setToken(res.data.access_token);
      } catch (err) {
        clearToken();
        throw err;
      }
    },
    async exchangeToken(code: string) {
      try {
        const res = await apiExchangeToken(code);
        setToken(res.data.access_token);
      } catch (err) {
        clearToken();
        throw err;
      }
    },
    logoutCallBack() {
      const appStore = useAppStore();
      this.resetInfo();
      clearToken();
      removeRouteListener();
      appStore.clearServerMenu();
    },
    async logout() {
      try {
        const res = await userLogout();
        this.logoutCallBack();
        if (isOidc() && res.data?.logout_url) {
          window.location.href = res.data.logout_url;
        }
      } catch {
        this.logoutCallBack();
      }
    },
  },
});

export default useUserStore;
