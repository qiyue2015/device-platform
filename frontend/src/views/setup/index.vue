<template>
  <div class="setup-page">
    <section class="setup-shell">
      <header class="setup-header">
        <div>
          <p class="eyebrow">{{ t('setup.eyebrow') }}</p>
          <h1>{{ t('setup.title') }}</h1>
          <p>{{ t('setup.subtitle') }}</p>
        </div>
        <a-tag :color="status?.needs_setup ? 'orange' : 'green'">
          {{ status?.needs_setup ? t('setup.status.pending') : t('setup.status.installed') }}
        </a-tag>
      </header>

      <a-steps :current="currentStep" class="setup-steps">
        <a-step v-for="step in steps" :key="step.key" :title="step.title" />
      </a-steps>

      <a-alert v-if="errorMessage" type="error" class="setup-error" :content="errorMessage" />

      <section v-if="currentStep === 0" class="setup-panel">
        <a-descriptions :column="1" bordered>
          <a-descriptions-item :label="t('setup.system.installDir')">
            {{ t('setup.system.installDirValue') }}
          </a-descriptions-item>
          <a-descriptions-item :label="t('setup.system.required')">
            PostgreSQL, Redis, JWT secret, admin user
          </a-descriptions-item>
          <a-descriptions-item label="WWTIOT">
            {{ form.wwtiot.dry_run ? t('setup.wwtiot.dryRun') : t('setup.wwtiot.live') }}
          </a-descriptions-item>
        </a-descriptions>
      </section>

      <section v-if="currentStep === 1" class="setup-panel">
        <a-form :model="form.database" layout="vertical">
          <a-form-item :label="t('setup.database.url')">
            <a-input v-model="form.database.url" allow-clear @input="dbConnected = false" />
          </a-form-item>
          <a-button type="primary" :loading="testingDb" @click="testDB">
            <template #icon><icon-check /></template>
            {{ dbConnected ? t('setup.action.tested') : t('setup.action.testDatabase') }}
          </a-button>
        </a-form>
      </section>

      <section v-if="currentStep === 2" class="setup-panel">
        <a-form :model="form.redis" layout="vertical">
          <a-form-item :label="t('setup.redis.url')">
            <a-input v-model="form.redis.url" allow-clear @input="redisConnected = false" />
          </a-form-item>
          <a-button type="primary" :loading="testingRedis" @click="testRedisConnection">
            <template #icon><icon-check /></template>
            {{ redisConnected ? t('setup.action.tested') : t('setup.action.testRedis') }}
          </a-button>
        </a-form>
      </section>

      <section v-if="currentStep === 3" class="setup-panel">
        <a-form :model="form.admin" layout="vertical">
          <a-form-item :label="t('setup.admin.email')">
            <a-input v-model="form.admin.email" allow-clear />
          </a-form-item>
          <a-form-item :label="t('setup.admin.displayName')">
            <a-input v-model="form.admin.display_name" allow-clear />
          </a-form-item>
          <a-form-item :label="t('setup.admin.password')">
            <a-input-password v-model="form.admin.password" />
          </a-form-item>
          <a-form-item :label="t('setup.admin.confirmPassword')">
            <a-input-password v-model="form.admin.confirm_password" />
          </a-form-item>
        </a-form>
      </section>

      <section v-if="currentStep === 4" class="setup-panel">
        <a-form :model="form" layout="vertical">
          <a-form-item :label="t('setup.server.addr')">
            <a-input v-model="form.server.addr" />
          </a-form-item>
          <a-form-item :label="t('setup.server.logLevel')">
            <a-select v-model="form.server.log_level">
              <a-option value="debug">debug</a-option>
              <a-option value="info">info</a-option>
              <a-option value="warn">warn</a-option>
              <a-option value="error">error</a-option>
            </a-select>
          </a-form-item>
          <a-form-item>
            <a-checkbox v-model="form.wwtiot.dry_run">{{ t('setup.wwtiot.dryRun') }}</a-checkbox>
          </a-form-item>
          <a-form-item label="WWTIOT API URL">
            <a-input v-model="form.wwtiot.api_url" />
          </a-form-item>
          <a-form-item label="WWTIOT User ID">
            <a-input v-model="form.wwtiot.user_id" />
          </a-form-item>
          <a-form-item label="WWTIOT User Key">
            <a-input-password v-model="form.wwtiot.user_key" />
          </a-form-item>
        </a-form>
      </section>

      <section v-if="currentStep === 5" class="setup-panel complete">
        <icon-check-circle class="complete-icon" />
        <h2>{{ t('setup.complete.title') }}</h2>
        <p>{{ t('setup.complete.desc') }}</p>
        <a-button type="primary" @click="router.replace({ name: 'login' })">
          {{ t('setup.action.login') }}
        </a-button>
      </section>

      <footer v-if="currentStep < 5" class="setup-actions">
        <a-button :disabled="currentStep === 0" @click="currentStep -= 1">{{ t('setup.action.prev') }}</a-button>
        <a-button v-if="currentStep < 4" type="primary" :disabled="!canProceed" @click="nextStep">
          {{ t('setup.action.next') }}
        </a-button>
        <a-button v-else type="primary" :disabled="!canProceed" :loading="installing" @click="performInstall">
          {{ t('setup.action.install') }}
        </a-button>
      </footer>
    </section>
  </div>
</template>

<script lang="ts" setup>
  import { computed, onMounted, reactive, ref } from 'vue';
  import { useI18n } from 'vue-i18n';
  import { useRouter } from 'vue-router';
  import { getSetupStatus, install, testDatabase, testRedis, SetupStatus } from '@/api/setup';
  import { resetSetupGuardCache } from '@/router/guard/setup';

  defineOptions({ name: 'SetupWizard' });

  const router = useRouter();
  const { t } = useI18n();
  const status = ref<SetupStatus>();
  const currentStep = ref(0);
  const dbConnected = ref(false);
  const redisConnected = ref(false);
  const testingDb = ref(false);
  const testingRedis = ref(false);
  const installing = ref(false);
  const errorMessage = ref('');

  const form = reactive({
    database: {
      url: 'postgres://postgres:postgres@localhost:5432/device_platform?sslmode=disable',
    },
    redis: {
      url: 'redis://localhost:6379/0',
    },
    admin: {
      email: '',
      display_name: 'Administrator',
      password: '',
      confirm_password: '',
    },
    server: {
      addr: ':8080',
      log_level: 'info',
    },
    wwtiot: {
      api_url: 'http://gps.wwtiot.com/api',
      dry_run: true,
      user_id: '',
      user_key: '',
    },
  });

  const steps = computed(() => [
    { key: 'system', title: t('setup.step.system') },
    { key: 'database', title: t('setup.step.database') },
    { key: 'redis', title: t('setup.step.redis') },
    { key: 'admin', title: t('setup.step.admin') },
    { key: 'install', title: t('setup.step.install') },
    { key: 'complete', title: t('setup.step.complete') },
  ]);

  const adminValid = computed(
    () =>
      form.admin.email.includes('@') &&
      form.admin.display_name.trim().length >= 2 &&
      form.admin.password.length >= 8 &&
      form.admin.password === form.admin.confirm_password
  );

  const runtimeValid = computed(
    () =>
      form.server.addr.trim() &&
      form.server.log_level &&
      form.wwtiot.api_url.trim() &&
      (form.wwtiot.dry_run || (form.wwtiot.user_id.trim() && form.wwtiot.user_key.trim()))
  );

  const canProceed = computed(() => {
    if (currentStep.value === 0) return true;
    if (currentStep.value === 1) return dbConnected.value;
    if (currentStep.value === 2) return redisConnected.value;
    if (currentStep.value === 3) return adminValid.value;
    if (currentStep.value === 4) return dbConnected.value && redisConnected.value && adminValid.value && runtimeValid.value;
    return true;
  });

  const showError = (error: unknown, fallback: string) => {
    const err = error as { response?: { data?: { message?: string; data?: { error?: string } } }; message?: string };
    errorMessage.value = err.response?.data?.message || err.response?.data?.data?.error || err.message || fallback;
  };

  const refreshStatus = async () => {
    const res = await getSetupStatus();
    status.value = res.data;
    if (!res.data.needs_setup) {
      router.replace({ name: 'login' });
    }
  };

  const testDB = async () => {
    testingDb.value = true;
    errorMessage.value = '';
    dbConnected.value = false;
    try {
      await testDatabase(form.database);
      dbConnected.value = true;
    } catch (error) {
      showError(error, t('setup.error.database'));
    } finally {
      testingDb.value = false;
    }
  };

  const testRedisConnection = async () => {
    testingRedis.value = true;
    errorMessage.value = '';
    redisConnected.value = false;
    try {
      await testRedis(form.redis);
      redisConnected.value = true;
    } catch (error) {
      showError(error, t('setup.error.redis'));
    } finally {
      testingRedis.value = false;
    }
  };

  const nextStep = () => {
    if (!canProceed.value) return;
    errorMessage.value = '';
    currentStep.value += 1;
  };

  const performInstall = async () => {
    installing.value = true;
    errorMessage.value = '';
    try {
      await install(form);
      resetSetupGuardCache();
      currentStep.value = 5;
      status.value = { needs_setup: false, installed: true, step: 'complete' };
    } catch (error) {
      showError(error, t('setup.error.install'));
    } finally {
      installing.value = false;
    }
  };

  onMounted(refreshStatus);
</script>

<style lang="less" scoped>
  .setup-page {
    min-height: 100vh;
    padding: 32px;
    background: var(--color-fill-2);
  }

  .setup-shell {
    max-width: 920px;
    margin: 0 auto;
    padding: 28px;
    background: var(--color-bg-2);
    border: 1px solid var(--color-border-2);
    border-radius: 8px;
  }

  .setup-header {
    display: flex;
    gap: 24px;
    align-items: flex-start;
    justify-content: space-between;

    h1 {
      margin: 4px 0 8px;
      color: var(--color-text-1);
      font-size: 26px;
      line-height: 34px;
    }

    p {
      margin: 0;
      color: var(--color-text-2);
    }
  }

  .eyebrow {
    color: rgb(var(--primary-6));
    font-weight: 600;
    font-size: 12px;
  }

  .setup-steps {
    margin-top: 28px;
  }

  .setup-error {
    margin-top: 20px;
  }

  .setup-panel {
    margin-top: 24px;
    padding: 20px;
    background: var(--color-fill-1);
    border-radius: 8px;
  }

  .setup-actions {
    display: flex;
    gap: 12px;
    justify-content: flex-end;
    margin-top: 24px;
  }

  .complete {
    text-align: center;
  }

  .complete-icon {
    color: rgb(var(--green-6));
    font-size: 48px;
  }
</style>
