<template>
  <div class="setup-page">
    <main class="setup-shell">
      <header class="setup-header">
        <h1>{{ t('setup.title') }}</h1>
      </header>

      <section class="setup-steps" :aria-label="t('setup.timeline.label')">
        <a-steps :current="currentStep + 1" label-placement="vertical" small changeable @change="handleStepChange">
          <a-step v-for="(step, index) in steps" :key="step.key" :title="step.title" :disabled="!isStepAvailable(index)" />
        </a-steps>
      </section>

      <section class="setup-card">
        <section v-if="currentStep === 0" class="form-stage">
          <a-form :model="form" layout="vertical">
            <div class="connection-list">
              <section class="connection-block">
                <h2>{{ t('setup.database.title') }}</h2>
                <div class="form-grid service-grid">
                  <a-form-item :label="t('setup.database.host')">
                    <a-input v-model="form.database.host" allow-clear @input="resetDatabaseConnection" />
                  </a-form-item>
                  <a-form-item :label="t('setup.database.port')">
                    <a-input-number
                      v-model="form.database.port"
                      :min="1"
                      :max="65535"
                      :precision="0"
                      model-event="input"
                      hide-button
                      @input="resetDatabaseConnection"
                    />
                  </a-form-item>
                  <a-form-item :label="t('setup.database.name')">
                    <a-input v-model="form.database.name" allow-clear @input="resetDatabaseConnection" />
                  </a-form-item>
                  <a-form-item :label="t('setup.database.username')">
                    <a-input v-model="form.database.username" allow-clear @input="resetDatabaseConnection" />
                  </a-form-item>
                  <a-form-item :label="t('setup.database.password')">
                    <a-input-password v-model="form.database.password" allow-clear @input="resetDatabaseConnection" />
                  </a-form-item>
                  <a-form-item :label="t('setup.database.sslMode')">
                    <a-select v-model="form.database.ssl_mode" @change="resetDatabaseConnection">
                      <a-option value="disable">disable</a-option>
                      <a-option value="require">require</a-option>
                    </a-select>
                  </a-form-item>
                </div>
                <div v-if="dbConnected || databaseErrorMessage" class="inline-status" aria-live="polite">
                  <span v-if="databaseErrorMessage" class="inline-status-text error">
                    <icon-exclamation-circle />
                    {{ databaseErrorMessage }}
                  </span>
                  <span v-else class="inline-status-text ok">
                    <icon-check-circle />
                    {{ t('setup.hint.connectionVerified') }}
                  </span>
                </div>
              </section>

              <section class="connection-block">
                <h2>{{ t('setup.redis.title') }}</h2>
                <div class="form-grid service-grid">
                  <a-form-item :label="t('setup.redis.host')">
                    <a-input v-model="form.redis.host" allow-clear @input="resetRedisConnection" />
                  </a-form-item>
                  <a-form-item :label="t('setup.redis.port')">
                    <a-input-number
                      v-model="form.redis.port"
                      :min="1"
                      :max="65535"
                      :precision="0"
                      model-event="input"
                      hide-button
                      @input="resetRedisConnection"
                    />
                  </a-form-item>
                  <a-form-item :label="t('setup.redis.database')">
                    <a-input-number
                      v-model="form.redis.database"
                      :min="0"
                      :precision="0"
                      model-event="input"
                      hide-button
                      @input="resetRedisConnection"
                    />
                  </a-form-item>
                  <a-form-item :label="t('setup.redis.password')">
                    <a-input-password v-model="form.redis.password" allow-clear @input="resetRedisConnection" />
                  </a-form-item>
                </div>
                <div v-if="redisConnected || redisErrorMessage" class="inline-status" aria-live="polite">
                  <span v-if="redisErrorMessage" class="inline-status-text error">
                    <icon-exclamation-circle />
                    {{ redisErrorMessage }}
                  </span>
                  <span v-else class="inline-status-text ok">
                    <icon-check-circle />
                    {{ t('setup.hint.connectionVerified') }}
                  </span>
                </div>
              </section>
            </div>
          </a-form>
        </section>

        <section v-if="currentStep === 1" class="form-stage">
          <a-form :model="form.admin" layout="vertical">
            <div class="form-grid">
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
            </div>
          </a-form>
          <div class="password-rules">
            <span class="password-rule" :class="{ ok: form.admin.email.includes('@') }">{{ t('setup.admin.rule.email') }}</span>
            <span class="password-rule" :class="{ ok: form.admin.password.length >= 8 }">{{
              t('setup.admin.rule.password')
            }}</span>
            <span
              class="password-rule"
              :class="{ ok: form.admin.password === form.admin.confirm_password && form.admin.password.length > 0 }"
            >
              {{ t('setup.admin.rule.confirm') }}
            </span>
          </div>
        </section>

        <section v-if="currentStep === 2" class="form-stage">
          <a-form :model="form.server" layout="vertical">
            <div class="form-grid">
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
            </div>
          </a-form>
        </section>

        <section v-if="currentStep === 3" class="complete">
          <div class="complete-mark">
            <icon-check-circle />
          </div>
          <h2>{{ t('setup.complete.title') }}</h2>
          <p>{{ t('setup.complete.desc') }}</p>
          <a-button type="primary" size="large" @click="router.replace({ name: 'login' })">
            {{ t('setup.action.login') }}
          </a-button>
        </section>
      </section>

      <footer v-if="currentStep < completeStepIndex" class="setup-actions">
        <span class="setup-action-message" aria-live="polite">{{ errorMessage }}</span>
        <div class="setup-action-buttons">
          <a-button v-if="currentStep > 0" @click="currentStep -= 1">{{ t('setup.action.prev') }}</a-button>
          <a-button
            v-if="currentStep < installStepIndex"
            type="primary"
            :disabled="!canProceed"
            :loading="currentStep === 0 && checkingServices"
            @click="nextStep"
          >
            {{ serviceActionLabel }}
          </a-button>
          <a-button v-else type="primary" :disabled="!canProceed" :loading="installing" @click="performInstall">
            {{ t('setup.action.install') }}
          </a-button>
        </div>
      </footer>
    </main>
  </div>
</template>

<script lang="ts" setup>
  import { computed, onMounted, reactive, ref } from 'vue';
  import { useI18n } from 'vue-i18n';
  import { useRouter } from 'vue-router';
  import { getSetupStatus, install, testDatabase, testRedis } from '@/api/setup';
  import type { SetupInstallRequest } from '@/api/setup';
  import { resetSetupGuardCache } from '@/router/guard/setup';

  defineOptions({ name: 'SetupWizard' });

  const router = useRouter();
  const { t } = useI18n();
  const currentStep = ref(0);
  const dbConnected = ref(false);
  const redisConnected = ref(false);
  const checkingServices = ref(false);
  const installing = ref(false);
  const errorMessage = ref('');
  const databaseErrorMessage = ref('');
  const redisErrorMessage = ref('');
  const installStepIndex = 2;
  const completeStepIndex = 3;

  const form = reactive({
    database: {
      host: 'localhost',
      port: 5432,
      name: '',
      username: '',
      password: '',
      ssl_mode: 'disable',
    },
    redis: {
      host: 'localhost',
      port: 6379,
      database: 0,
      password: '',
    },
    admin: {
      email: '',
      display_name: '',
      password: '',
      confirm_password: '',
    },
    server: {
      addr: ':8080',
      log_level: 'info',
    },
  });

  const steps = computed(() => [
    {
      key: 'services',
      title: t('setup.step.services'),
    },
    {
      key: 'admin',
      title: t('setup.step.admin'),
    },
    {
      key: 'install',
      title: t('setup.step.install'),
    },
    {
      key: 'complete',
      title: t('setup.step.complete'),
    },
  ]);

  const servicesValid = computed(() => dbConnected.value && redisConnected.value);
  const serviceActionLabel = computed(() =>
    currentStep.value === 0 && !servicesValid.value ? t('setup.action.detect') : t('setup.action.next')
  );
  const isValidPort = (value?: number) => typeof value === 'number' && Number.isInteger(value) && value >= 1 && value <= 65535;
  const isValidRedisDatabase = (value?: number) => typeof value === 'number' && Number.isInteger(value) && value >= 0;
  const servicesFormValid = computed(() =>
    Boolean(
      form.database.host.trim() &&
        isValidPort(form.database.port) &&
        form.database.name.trim() &&
        form.database.username.trim() &&
        form.database.ssl_mode &&
        form.redis.host.trim() &&
        isValidPort(form.redis.port) &&
        isValidRedisDatabase(form.redis.database)
    )
  );

  const adminValid = computed(
    () =>
      form.admin.email.includes('@') &&
      form.admin.display_name.trim().length >= 2 &&
      form.admin.password.length >= 8 &&
      form.admin.password === form.admin.confirm_password
  );

  const runtimeValid = computed(() => Boolean(form.server.addr.trim() && form.server.log_level));

  const canProceed = computed(() => {
    if (currentStep.value === 0) return servicesFormValid.value && !checkingServices.value;
    if (currentStep.value === 1) return adminValid.value;
    if (currentStep.value === installStepIndex) return servicesValid.value && adminValid.value && runtimeValid.value;
    return true;
  });

  const isStepAvailable = (index: number) => {
    if (index <= currentStep.value) return true;
    if (index === 1) return servicesValid.value;
    if (index === installStepIndex) return servicesValid.value && adminValid.value;
    if (index === completeStepIndex) return currentStep.value === completeStepIndex;
    return false;
  };

  const goToStep = (index: number) => {
    if (!isStepAvailable(index)) return;
    errorMessage.value = '';
    currentStep.value = index;
  };

  const handleStepChange = (step: number) => {
    goToStep(step - 1);
  };

  const getErrorMessage = (error: unknown, fallback: string) => {
    const err = error as { response?: { data?: { message?: string; data?: { error?: string } } }; message?: string };
    return err.response?.data?.message || err.response?.data?.data?.error || err.message || fallback;
  };

  const showError = (error: unknown, fallback: string) => {
    errorMessage.value = getErrorMessage(error, fallback);
  };

  const formatURLHost = (host: string) => {
    const value = host.trim();
    if (value.includes(':') && !value.startsWith('[')) {
      return `[${value}]`;
    }
    return value;
  };

  const buildDatabaseURL = () => {
    const { host, name, password, port, ssl_mode: sslMode, username } = form.database;
    const params = new URLSearchParams({ sslmode: sslMode });
    const encodedUsername = encodeURIComponent(username.trim());
    const encodedPassword = encodeURIComponent(password);
    const encodedName = encodeURIComponent(name.trim());
    return `postgres://${encodedUsername}:${encodedPassword}@${formatURLHost(
      host
    )}:${port}/${encodedName}?${params.toString()}`;
  };

  const buildRedisURL = () => {
    const { database, host, password, port } = form.redis;
    const auth = password ? `:${encodeURIComponent(password)}@` : '';
    return `redis://${auth}${formatURLHost(host)}:${port}/${database}`;
  };

  const buildInstallPayload = (): SetupInstallRequest => ({
    database: {
      url: buildDatabaseURL(),
    },
    redis: {
      url: buildRedisURL(),
    },
    admin: { ...form.admin },
    server: { ...form.server },
  });

  const refreshStatus = async () => {
    const res = await getSetupStatus();
    if (!res.data.needs_setup) {
      router.replace({ name: 'login' });
    }
  };

  const resetDatabaseConnection = () => {
    dbConnected.value = false;
    databaseErrorMessage.value = '';
    errorMessage.value = '';
  };

  const resetRedisConnection = () => {
    redisConnected.value = false;
    redisErrorMessage.value = '';
    errorMessage.value = '';
  };

  const verifyServices = async () => {
    const payload = buildInstallPayload();
    checkingServices.value = true;
    errorMessage.value = '';
    databaseErrorMessage.value = '';
    redisErrorMessage.value = '';
    dbConnected.value = false;
    redisConnected.value = false;

    try {
      await testDatabase(payload.database);
      dbConnected.value = true;
    } catch (error) {
      databaseErrorMessage.value = getErrorMessage(error, t('setup.error.database'));
      checkingServices.value = false;
      return false;
    }

    try {
      await testRedis(payload.redis);
      redisConnected.value = true;
    } catch (error) {
      redisErrorMessage.value = getErrorMessage(error, t('setup.error.redis'));
      checkingServices.value = false;
      return false;
    }

    checkingServices.value = false;
    return true;
  };

  const nextStep = async () => {
    if (!canProceed.value) return;
    errorMessage.value = '';
    if (currentStep.value === 0 && !servicesValid.value) {
      await verifyServices();
      return;
    }
    currentStep.value += 1;
  };

  const performInstall = async () => {
    installing.value = true;
    errorMessage.value = '';
    try {
      await install(buildInstallPayload());
      resetSetupGuardCache();
      currentStep.value = completeStepIndex;
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
    position: relative;
    display: grid;
    min-height: 100vh;
    padding: 48px;
    overflow: auto;
    background: linear-gradient(135deg, #07111f 0%, #0f172a 50%, #101820 100%);
    place-items: center;

    &::before {
      position: fixed;
      inset: 0;
      background-image: linear-gradient(rgb(255 255 255 / 4%) 1px, transparent 1px),
        linear-gradient(90deg, rgb(255 255 255 / 4%) 1px, transparent 1px);
      background-size: 40px 40px;
      mask-image: linear-gradient(to bottom, rgb(0 0 0 / 82%), transparent 82%);
      content: '';
      pointer-events: none;
    }
  }

  .setup-shell {
    position: relative;
    z-index: 1;
    width: min(720px, calc(100vw - 96px));
    background: #f8fafc;
    border: 1px solid rgb(255 255 255 / 42%);
    border-radius: 8px;
    box-shadow: 0 28px 80px rgb(0 0 0 / 34%);
  }

  .setup-header {
    padding: 28px 28px 20px;

    h1 {
      margin: 0;
      color: #0f172a;
      font-weight: 750;
      font-size: 26px;
      line-height: 34px;
      letter-spacing: 0;
    }
  }

  .setup-steps {
    padding: 0 28px;
  }

  .setup-card {
    min-height: 306px;
    margin: 20px 28px 0;
    padding: 24px;
    background: #fff;
    border: 1px solid #e2e8f0;
    border-radius: 8px;
  }

  .connection-list {
    display: grid;
    gap: 26px;
  }

  .connection-block {
    h2 {
      display: flex;
      gap: 10px;
      align-items: center;
      margin: 0 0 16px;
      color: #0f172a;
      font-weight: 750;
      font-size: 15px;
      line-height: 22px;
      letter-spacing: 0;

      &::before {
        display: block;
        width: 4px;
        height: 16px;
        background: #2563eb;
        border-radius: 999px;
        content: '';
      }
    }

    :deep(.arco-form-item) {
      margin-bottom: 10px;
    }
  }

  .service-grid {
    grid-template-columns: minmax(0, 1fr) 136px;
    gap: 2px 16px;
  }

  .form-grid {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 2px 18px;
  }

  .inline-status {
    min-height: 22px;
  }

  .inline-status-text {
    display: inline-flex;
    gap: 8px;
    align-items: center;
    color: #64748b;
    font-size: 13px;

    svg {
      flex: 0 0 auto;
    }

    &.ok {
      color: #047857;
    }

    &.error {
      color: #dc2626;
    }
  }

  .password-rules {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
    margin-top: 6px;
  }

  .password-rule {
    padding: 6px 10px;
    color: #64748b;
    font-size: 12px;
    background: #f1f5f9;
    border-radius: 999px;

    &.ok {
      color: #047857;
      background: #ecfdf5;
    }
  }

  .complete {
    display: grid;
    min-height: 258px;
    place-items: center;
    text-align: center;

    h2 {
      margin: 18px 0 8px;
      color: #0f172a;
      font-size: 28px;
      line-height: 36px;
      letter-spacing: 0;
    }

    p {
      max-width: 420px;
      margin: 0 0 24px;
      color: #64748b;
      line-height: 22px;
    }
  }

  .complete-mark {
    display: grid;
    width: 72px;
    height: 72px;
    color: #fff;
    font-size: 40px;
    background: #2563eb;
    border-radius: 8px;
    place-items: center;
  }

  .setup-actions {
    display: flex;
    gap: 12px;
    align-items: center;
    justify-content: space-between;
    min-height: 72px;
    padding: 16px 28px 28px;
  }

  .setup-action-message {
    min-width: 0;
    color: #dc2626;
    font-size: 13px;
    line-height: 20px;
  }

  .setup-action-buttons {
    display: flex;
    flex: 0 0 auto;
    gap: 12px;
  }

  :deep(.arco-steps) {
    width: 100%;
  }

  :deep(.arco-steps-item-content) {
    min-width: 0;
  }

  :deep(.arco-steps-item-title) {
    color: #334155;
    font-weight: 650;
    letter-spacing: 0;
    white-space: nowrap;
  }

  :deep(.arco-steps-item-wait .arco-steps-item-title),
  :deep(.arco-steps-item-disabled .arco-steps-item-title) {
    color: #94a3b8;
  }

  :deep(.arco-steps-item-disabled) {
    cursor: not-allowed;
  }

  :deep(.arco-input-wrapper),
  :deep(.arco-input-number),
  :deep(.arco-select-view-single) {
    width: 100%;
    min-height: 42px;
    background: #fff;
    border-color: #cbd5e1;
    border-radius: 8px;
  }

  :deep(.arco-form-item-label-col > label) {
    color: #334155;
    font-weight: 650;
  }

  :deep(.arco-btn) {
    min-height: 36px;
    border-radius: 8px;
  }

  :deep(.arco-btn-primary) {
    color: #fff;
    font-weight: 700;
    background: #2563eb;
    border: 0;

    &:hover {
      color: #fff;
      background: #1d4ed8;
    }

    &[disabled] {
      color: rgb(255 255 255 / 72%);
      background: #94a3b8;
    }
  }

  :deep(.arco-btn:focus-visible),
  :deep(.arco-input-wrapper:focus-within),
  :deep(.arco-select-view-single:focus-within) {
    outline: 2px solid rgb(37 99 235 / 46%);
    outline-offset: 2px;
  }

  @media (width <= 760px) {
    .setup-page {
      padding: 20px;
    }

    .setup-shell {
      width: calc(100vw - 40px);
    }

    .setup-header,
    .setup-steps,
    .setup-actions {
      padding-right: 20px;
      padding-left: 20px;
    }

    .setup-card,
    .setup-error {
      margin-right: 20px;
      margin-left: 20px;
    }

    .form-grid {
      grid-template-columns: 1fr;
    }

    .inline-status {
      flex-direction: column;
      align-items: stretch;
    }
  }
</style>
