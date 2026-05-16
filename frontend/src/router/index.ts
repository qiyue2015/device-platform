import { createRouter, createWebHistory } from 'vue-router';
import NProgress from 'nprogress'; // progress bar
import 'nprogress/nprogress.css';

import { appRoutes } from './routes';
import { REDIRECT_MAIN, NOT_FOUND_ROUTE } from './routes/base';
import createRouteGuard from './guard';

NProgress.configure({ showSpinner: false }); // NProgress Configuration

const router = createRouter({
  history: createWebHistory(),
  routes: [
    {
      path: '/',
      redirect: '/dashboard/workplace',
    },
    {
      path: '/setup',
      name: 'setup',
      component: () => import('@/views/setup/index.vue'),
      meta: {
        requiresAuth: false,
        locale: 'setup.title',
      },
    },
    {
      path: '/auth/login',
      name: 'login',
      component: () => import('@/views/auth/index.vue'),
      meta: {
        requiresAuth: false,
        locale: 'auth.login',
      },
    },
    {
      path: '/auth/forgot-password',
      name: 'forgot-password',
      component: () => import('@/views/auth/index.vue'),
      meta: {
        requiresAuth: false,
        locale: 'auth.forgot.title',
      },
    },
    {
      path: '/auth/reset-password',
      name: 'reset-password',
      component: () => import('@/views/auth/index.vue'),
      meta: {
        requiresAuth: false,
        locale: 'auth.reset.title',
      },
    },
    ...appRoutes,
    REDIRECT_MAIN,
    NOT_FOUND_ROUTE,
  ],
  scrollBehavior() {
    return { top: 0 };
  },
});

createRouteGuard(router);

export default router;
