import { DEFAULT_LAYOUT } from '../base';
import { AppRouteRecordRaw } from '../types';

const routes: AppRouteRecordRaw[] = [
  {
    path: '/projects',
    name: 'Projects',
    component: DEFAULT_LAYOUT,
    meta: {
      locale: 'menu.projects',
      icon: 'icon-folder',
      requiresAuth: true,
      order: 1,
    },
    children: [
      {
        path: 'index',
        name: 'ProjectsIndex',
        component: () => import('@/views/projects/index.vue'),
        meta: { locale: 'menu.projects.index', requiresAuth: true, roles: ['*'] },
      },
    ],
  },
  {
    path: '/devices',
    name: 'Devices',
    component: DEFAULT_LAYOUT,
    meta: {
      locale: 'menu.devices',
      icon: 'icon-storage',
      requiresAuth: true,
      order: 2,
    },
    children: [
      {
        path: 'index',
        name: 'DevicesIndex',
        component: () => import('@/views/devices/index.vue'),
        meta: { locale: 'menu.devices.index', requiresAuth: true, roles: ['*'] },
      },
    ],
  },
  {
    path: '/commands',
    name: 'Commands',
    component: DEFAULT_LAYOUT,
    meta: {
      locale: 'menu.commands',
      icon: 'icon-send',
      requiresAuth: true,
      order: 3,
    },
    children: [
      {
        path: 'index',
        name: 'CommandsIndex',
        component: () => import('@/views/commands/index.vue'),
        meta: { locale: 'menu.commands.index', requiresAuth: true, roles: ['*'] },
      },
    ],
  },
  {
    path: '/webhooks',
    name: 'Webhooks',
    component: DEFAULT_LAYOUT,
    meta: {
      locale: 'menu.webhooks',
      icon: 'icon-link',
      requiresAuth: true,
      order: 4,
    },
    children: [
      {
        path: 'index',
        name: 'WebhooksIndex',
        component: () => import('@/views/webhooks/index.vue'),
        meta: { locale: 'menu.webhooks.index', requiresAuth: true, roles: ['*'] },
      },
    ],
  },
  {
    path: '/audit-logs',
    name: 'AuditLogs',
    component: DEFAULT_LAYOUT,
    meta: {
      locale: 'menu.auditLogs',
      icon: 'icon-file',
      requiresAuth: true,
      order: 5,
    },
    children: [
      {
        path: 'index',
        name: 'AuditLogsIndex',
        component: () => import('@/views/audit-logs/index.vue'),
        meta: { locale: 'menu.auditLogs.index', requiresAuth: true, roles: ['*'] },
      },
    ],
  },
  {
    path: '/simulator',
    name: 'Simulator',
    component: DEFAULT_LAYOUT,
    meta: {
      locale: 'menu.simulator',
      icon: 'icon-thunderbolt',
      requiresAuth: true,
      order: 6,
    },
    children: [
      {
        path: 'index',
        name: 'SimulatorIndex',
        component: () => import('@/views/simulator/index.vue'),
        meta: { locale: 'menu.simulator.index', requiresAuth: true, roles: ['*'] },
      },
    ],
  },
];

export default routes;
