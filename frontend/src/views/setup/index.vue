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

      <a-alert v-if="errorMessage" type="error" class="setup-error" :content="errorMessage" show-icon />

      <section class="setup-card">
        <section v-if="currentStep === 0" class="form-stage">
          <a-form :model="form" layout="vertical">
            <div class="connection-list">
              <section class="connection-block">
                <a-form-item :label="t('setup.database.url')">
                  <a-input v-model="form.database.url" allow-clear @input="dbConnected = false">
                    <template #append>
                      <a-button class="connection-test-button" type="primary" :loading="testingDb" @click="testDB">
                        {{ t('setup.action.test') }}
                      </a-button>
                    </template>
                  </a-input>
                </a-form-item>
                <div class="inline-status" aria-live="polite">
                  <span class="inline-status-text" :class="{ ok: dbConnected }">
                    <icon-check-circle v-if="dbConnected" />
                    <icon-clock-circle v-else />
                    {{ dbConnected ? t('setup.action.tested') : t('setup.hint.needsTest') }}
                  </span>
                </div>
              </section>

              <section class="connection-block">
                <a-form-item :label="t('setup.redis.url')">
                  <a-input v-model="form.redis.url" allow-clear @input="redisConnected = false">
                    <template #append>
                      <a-button
                        class="connection-test-button"
                        type="primary"
                        :loading="testingRedis"
                        @click="testRedisConnection"
                      >
                        {{ t('setup.action.test') }}
                      </a-button>
                    </template>
                  </a-input>
                </a-form-item>
                <div class="inline-status" aria-live="polite">
                  <span class="inline-status-text" :class="{ ok: redisConnected }">
                    <icon-check-circle v-if="redisConnected" />
                    <icon-clock-circle v-else />
                    {{ redisConnected ? t('setup.action.tested') : t('setup.hint.needsTest') }}
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
        <a-button v-if="currentStep > 0" @click="currentStep -= 1">{{ t('setup.action.prev') }}</a-button>
        <a-button v-if="currentStep < installStepIndex" type="primary" :disabled="!canProceed" @click="nextStep">
          {{ t('setup.action.next') }}
        </a-button>
        <a-button v-else type="primary" :disabled="!canProceed" :loading="installing" @click="performInstall">
          {{ t('setup.action.install') }}
        </a-button>
      </footer>
    </main>
  </div>
</template>

<script lang="ts" setup>
  import { computed, onMounted, reactive, ref } from 'vue';
  import { useI18n } from 'vue-i18n';
  import { useRouter } from 'vue-router';
  import { getSetupStatus, install, testDatabase, testRedis } from '@/api/setup';
  import { resetSetupGuardCache } from '@/router/guard/setup';

  defineOptions({ name: 'SetupWizard' });

  const router = useRouter();
  const { t } = useI18n();
  const currentStep = ref(0);
  const dbConnected = ref(false);
  const redisConnected = ref(false);
  const testingDb = ref(false);
  const testingRedis = ref(false);
  const installing = ref(false);
  const errorMessage = ref('');
  const installStepIndex = 2;
  const completeStepIndex = 3;

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

  const adminValid = computed(
    () =>
      form.admin.email.includes('@') &&
      form.admin.display_name.trim().length >= 2 &&
      form.admin.password.length >= 8 &&
      form.admin.password === form.admin.confirm_password
  );

  const runtimeValid = computed(() => Boolean(form.server.addr.trim() && form.server.log_level));

  const canProceed = computed(() => {
    if (currentStep.value === 0) return servicesValid.value;
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

  const showError = (error: unknown, fallback: string) => {
    const err = error as { response?: { data?: { message?: string; data?: { error?: string } } }; message?: string };
    errorMessage.value = err.response?.data?.message || err.response?.data?.data?.error || err.message || fallback;
  };

  const refreshStatus = async () => {
    const res = await getSetupStatus();
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

  .setup-error {
    margin: 18px 28px 0;
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
    gap: 18px;
  }

  .connection-block {
    padding-bottom: 18px;
    border-bottom: 1px solid #e2e8f0;

    &:last-child {
      padding-bottom: 0;
      border-bottom: 0;
    }

    :deep(.arco-form-item) {
      margin-bottom: 10px;
    }
  }

  .connection-test-button {
    min-width: 72px;
    height: 42px;
    border-radius: 0 8px 8px 0;
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
    justify-content: flex-end;
    min-height: 72px;
    padding: 16px 28px 28px;
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
  :deep(.arco-input-outer),
  :deep(.arco-select-view-single) {
    min-height: 42px;
    background: #fff;
    border-color: #cbd5e1;
    border-radius: 8px;
  }

  :deep(.arco-input-outer .arco-input-wrapper) {
    border-radius: 8px 0 0 8px;
  }

  :deep(.arco-input-append) {
    padding: 0;
    background: transparent;
    border-color: #2563eb;
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
