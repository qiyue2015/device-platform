import type { Router } from 'vue-router';
import { getSetupStatus } from '@/api/setup';

let setupChecked = false;
let needsSetup = false;

export default function setupInstallGuard(router: Router) {
  router.beforeEach(async (to, from, next) => {
    if (!setupChecked) {
      try {
        const res = await getSetupStatus();
        needsSetup = res.data.needs_setup;
      } catch {
        needsSetup = false;
      } finally {
        setupChecked = true;
      }
    }

    if (needsSetup && to.name !== 'setup') {
      next({ name: 'setup', replace: true });
      return;
    }

    if (!needsSetup && to.name === 'setup') {
      next({ name: 'login', replace: true });
      return;
    }

    next();
  });
}

export function resetSetupGuardCache() {
  setupChecked = false;
}
