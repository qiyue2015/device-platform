import type { Router, LocationQueryRaw } from 'vue-router';
import NProgress from 'nprogress'; // progress bar

import { useUserStore } from '@/store';
import { isLogin } from '@/utils/auth';
import { isOidc, redirectToOidcLogin } from '@/utils/auth-strategy';
import { DEFAULT_ROUTE_NAME } from '../constants';

export default function setupUserLoginInfoGuard(router: Router) {
  router.beforeEach(async (to, from, next) => {
    NProgress.start();
    const userStore = useUserStore();
    if (isLogin()) {
      if (!to.meta.requiresAuth) {
        next();
      } else if (userStore.roles.length > 0) {
        next();
      } else {
        try {
          await userStore.info();
          next();
        } catch (error) {
          await userStore.logout();
          if (isOidc()) {
            // store.logout will redirect to OIDC provider
            return;
          }
          next({
            name: 'login',
            query: {
              redirect: to.name,
              ...to.query,
            } as LocationQueryRaw,
          });
        }
      }
    } else if (isOidc()) {
      // OIDC mode: exchange code in guard to avoid page flash
      if (to.name === 'login' && to.query.code) {
        try {
          await userStore.exchangeToken(to.query.code as string);
          await userStore.info();
          const redirect = (to.query.redirect as string) || undefined;
          next({ name: redirect || DEFAULT_ROUTE_NAME, replace: true });
        } catch (err: any) {
          // Exchange failed, let auth view show error
          next({
            name: 'login',
            query: { oidcError: err?.message || '登录失败，请重试' },
            replace: true,
          });
        }
      } else {
        // Redirect to backend OIDC login
        redirectToOidcLogin();
      }
    } else {
      if (!to.meta.requiresAuth) {
        next();
        return;
      }
      next({
        name: 'login',
        query: {
          redirect: to.name,
          ...to.query,
        } as LocationQueryRaw,
      });
    }
  });
}
