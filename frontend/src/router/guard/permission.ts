import type { Router } from 'vue-router';
import NProgress from 'nprogress'; // progress bar

import usePermission from '@/hooks/permission';
import { useUserStore } from '@/store';
import { appRoutes } from '../routes';
import { NOT_FOUND } from '../constants';

export default function setupPermissionGuard(router: Router) {
  router.beforeEach((to, from, next) => {
    const userStore = useUserStore();
    const Permission = usePermission();
    const permissionsAllow = Permission.accessRouter(to);
    if (!to.meta.requiresAuth) {
      next();
      NProgress.done();
      return;
    }

    if (permissionsAllow) next();
    else {
      const destination = Permission.findFirstPermissionRoute(appRoutes, userStore.roles) || NOT_FOUND;
      next(destination);
    }
    NProgress.done();
  });
}
