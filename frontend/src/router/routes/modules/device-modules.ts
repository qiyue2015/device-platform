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
      menuGroup: 'menu.group.resources',
      menuGroupOrder: 1,
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
      menuGroup: 'menu.group.resources',
      menuGroupOrder: 1,
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
    path: '/providers',
    name: 'Providers',
    component: DEFAULT_LAYOUT,
    meta: {
      locale: 'menu.providers',
      icon: 'icon-apps',
      requiresAuth: true,
      menuGroup: 'menu.group.resources',
      menuGroupOrder: 1,
      order: 3,
    },
    children: [
      {
        path: 'index',
        name: 'ProvidersIndex',
        component: () => import('@/views/providers/index.vue'),
        meta: { locale: 'menu.providers.index', requiresAuth: true, roles: ['*'] },
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
      menuGroup: 'menu.group.commands',
      menuGroupOrder: 2,
      order: 4,
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
      menuGroup: 'menu.group.commands',
      menuGroupOrder: 2,
      order: 5,
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
    path: '/events',
    name: 'Events',
    component: DEFAULT_LAYOUT,
    meta: {
      locale: 'menu.events',
      icon: 'icon-notification',
      requiresAuth: true,
      menuGroup: 'menu.group.operations',
      menuGroupOrder: 3,
      order: 6,
    },
    children: [
      {
        path: 'index',
        name: 'EventsIndex',
        component: () => import('@/views/events/index.vue'),
        meta: { locale: 'menu.events.index', requiresAuth: true, roles: ['*'] },
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
      menuGroup: 'menu.group.operations',
      menuGroupOrder: 3,
      order: 7,
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
      menuGroup: 'menu.group.operations',
      menuGroupOrder: 3,
      order: 8,
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
