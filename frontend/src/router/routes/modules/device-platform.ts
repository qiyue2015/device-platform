import { DEFAULT_LAYOUT } from '../base';
import { AppRouteRecordRaw } from '../types';

const DEVICE_PLATFORM: AppRouteRecordRaw = {
  path: '/device-platform',
  name: 'DevicePlatform',
  component: DEFAULT_LAYOUT,
  meta: {
    locale: 'menu.devicePlatform',
    icon: 'icon-thunderbolt',
    requiresAuth: true,
    order: 1,
  },
  children: [
    {
      path: 'console',
      name: 'DevicePlatformConsole',
      component: () => import('@/views/device-platform/index.vue'),
      meta: {
        locale: 'menu.devicePlatform.console',
        icon: 'icon-storage',
        requiresAuth: true,
        roles: ['*'],
      },
    },
  ],
};

export default DEVICE_PLATFORM;
