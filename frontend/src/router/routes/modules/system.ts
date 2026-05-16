import { DEFAULT_LAYOUT } from '../base';
import { AppRouteRecordRaw } from '../types';

const SYSTEM: AppRouteRecordRaw = {
  path: '/system',
  name: 'system',
  component: DEFAULT_LAYOUT,
  meta: {
    locale: 'menu.system',
    icon: 'icon-settings',
    requiresAuth: true,
    order: 8,
  },
  children: [
    {
      path: 'user',
      name: 'SystemUser',
      component: () => import('@/views/system/user/index.vue'),
      meta: {
        locale: 'menu.system.user',
        icon: 'icon-user',
        requiresAuth: true,
        roles: ['super-admin', 'admin'],
      },
    },
    {
      path: 'role',
      name: 'SystemRole',
      component: () => import('@/views/system/role/index.vue'),
      meta: {
        locale: 'menu.system.role',
        icon: 'icon-user-group',
        requiresAuth: true,
        roles: ['super-admin', 'admin'],
      },
    },
    {
      path: 'menu',
      name: 'SystemMenu',
      component: () => import('@/views/system/menu/index.vue'),
      meta: {
        locale: 'menu.system.menu',
        icon: 'icon-menu',
        requiresAuth: true,
        roles: ['super-admin', 'admin'],
      },
    },
    {
      path: 'dict',
      name: 'SystemDict',
      component: () => import('@/views/system/dict/index.vue'),
      meta: {
        locale: 'menu.system.dict',
        icon: 'icon-book',
        requiresAuth: true,
        roles: ['super-admin', 'admin'],
      },
    },
    {
      path: 'log',
      name: 'SystemLog',
      component: () => import('@/views/system/log/index.vue'),
      meta: {
        locale: 'menu.system.log',
        icon: 'icon-file',
        requiresAuth: true,
        roles: ['super-admin', 'admin'],
      },
    },
  ],
};

export default SYSTEM;
