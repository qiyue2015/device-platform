<template>
  <div class="setup-page">
    <div class="setup-page-grid" />

    <main class="setup-shell">
      <aside class="setup-rail">
        <header class="brand-lockup">
          <div class="brand-mark">
            <span class="brand-mark-dot" />
            <span class="brand-mark-line" />
          </div>
          <div>
            <p class="eyebrow">{{ t('setup.eyebrow') }}</p>
            <h1>{{ t('setup.title') }}</h1>
          </div>
        </header>

        <p class="setup-subtitle">{{ t('setup.subtitle') }}</p>

        <div class="status-chip" :class="{ 'status-chip-ready': status?.needs_setup === false }">
          <span class="status-chip-pulse" />
          {{ status?.needs_setup === false ? t('setup.status.installed') : t('setup.status.pending') }}
        </div>

        <nav class="setup-timeline" :aria-label="t('setup.timeline.label')">
          <button
            v-for="(step, index) in steps"
            :key="step.key"
            class="timeline-step"
            :class="{
              'timeline-step-active': currentStep === index,
              'timeline-step-done': index < currentStep,
              'timeline-step-locked': index > currentStep && !isStepAvailable(index),
            }"
            type="button"
            :disabled="!isStepAvailable(index)"
            @click="goToStep(index)"
          >
            <span class="timeline-step-index">
              <icon-check v-if="index < currentStep" />
              <template v-else>{{ index + 1 }}</template>
            </span>
            <span>
              <strong>{{ step.title }}</strong>
              <small>{{ step.description }}</small>
            </span>
          </button>
        </nav>
      </aside>

      <section class="setup-workbench">
        <header class="workbench-header">
          <div>
            <p class="section-kicker">{{ activeStep?.kicker }}</p>
            <h2>{{ activeStep?.heading }}</h2>
            <p>{{ activeStep?.summary }}</p>
          </div>
          <div class="step-meter">
            <span>{{ currentStep + 1 }}</span>
            <small>/ {{ steps.length }}</small>
          </div>
        </header>

        <a-alert v-if="errorMessage" type="error" class="setup-error" :content="errorMessage" show-icon />

        <section class="setup-card">
          <header class="console-topline">
            <div class="console-title">
              <span class="console-dot" />
              <strong>{{ activeStep?.title }}</strong>
            </div>
            <div class="console-count">
              <strong>{{ readyCount }}/4</strong>
              <span>{{ t('setup.signal.title') }}</span>
            </div>
          </header>

          <section class="readiness-strip" :aria-label="t('setup.signal.title')">
            <span
              v-for="item in readinessItems"
              :key="item.key"
              class="readiness-pill"
              :class="{ 'readiness-pill-ok': item.ok }"
            >
              <icon-check-circle v-if="item.ok" />
              <icon-clock-circle v-else />
              {{ item.label }}
            </span>
          </section>

          <div class="setup-card-body">
            <section v-if="currentStep === 0" class="preflight-layout">
              <div class="preflight-copy">
                <div class="terminal-card">
                  <div class="terminal-card-bar">
                    <span />
                    <span />
                    <span />
                  </div>
                  <dl>
                    <div>
                      <dt>{{ t('setup.system.installDir') }}</dt>
                      <dd>{{ t('setup.system.installDirValue') }}</dd>
                    </div>
                    <div>
                      <dt>{{ t('setup.system.required') }}</dt>
                      <dd>PostgreSQL / Redis / JWT / Admin</dd>
                    </div>
                    <div>
                      <dt>WWTIOT</dt>
                      <dd>{{ form.wwtiot.dry_run ? t('setup.wwtiot.dryRun') : t('setup.wwtiot.live') }}</dd>
                    </div>
                  </dl>
                </div>
              </div>

              <div class="preflight-stack">
                <article v-for="item in systemCards" :key="item.key" class="preflight-item">
                  <span class="preflight-item-icon"><component :is="item.icon" /></span>
                  <div>
                    <strong>{{ item.title }}</strong>
                    <p>{{ item.desc }}</p>
                  </div>
                </article>
              </div>
            </section>

            <section v-if="currentStep === 1" class="form-stage">
              <a-form :model="form.database" layout="vertical">
                <a-form-item :label="t('setup.database.url')">
                  <a-input v-model="form.database.url" allow-clear @input="dbConnected = false" />
                </a-form-item>
                <div class="inline-status">
                  <span class="inline-status-text" :class="{ ok: dbConnected }">
                    <icon-check-circle v-if="dbConnected" />
                    <icon-clock-circle v-else />
                    {{ dbConnected ? t('setup.action.tested') : t('setup.hint.needsTest') }}
                  </span>
                  <a-button type="primary" :loading="testingDb" @click="testDB">
                    <template #icon><icon-check /></template>
                    {{ dbConnected ? t('setup.action.tested') : t('setup.action.testDatabase') }}
                  </a-button>
                </div>
              </a-form>
            </section>

            <section v-if="currentStep === 2" class="form-stage">
              <a-form :model="form.redis" layout="vertical">
                <a-form-item :label="t('setup.redis.url')">
                  <a-input v-model="form.redis.url" allow-clear @input="redisConnected = false" />
                </a-form-item>
                <div class="inline-status">
                  <span class="inline-status-text" :class="{ ok: redisConnected }">
                    <icon-check-circle v-if="redisConnected" />
                    <icon-clock-circle v-else />
                    {{ redisConnected ? t('setup.action.tested') : t('setup.hint.needsTest') }}
                  </span>
                  <a-button type="primary" :loading="testingRedis" @click="testRedisConnection">
                    <template #icon><icon-check /></template>
                    {{ redisConnected ? t('setup.action.tested') : t('setup.action.testRedis') }}
                  </a-button>
                </div>
              </a-form>
            </section>

            <section v-if="currentStep === 3" class="form-stage">
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
                <span class="password-rule" :class="{ ok: form.admin.email.includes('@') }">{{
                  t('setup.admin.rule.email')
                }}</span>
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

            <section v-if="currentStep === 4" class="form-stage">
              <a-form :model="form" layout="vertical">
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

                <div class="mode-switch">
                  <div>
                    <strong>{{ form.wwtiot.dry_run ? t('setup.wwtiot.dryRun') : t('setup.wwtiot.live') }}</strong>
                    <p>{{ t('setup.wwtiot.modeHint') }}</p>
                  </div>
                  <a-switch v-model="form.wwtiot.dry_run" />
                </div>

                <div class="form-grid">
                  <a-form-item label="WWTIOT API URL">
                    <a-input v-model="form.wwtiot.api_url" />
                  </a-form-item>
                  <a-form-item label="WWTIOT User ID">
                    <a-input v-model="form.wwtiot.user_id" />
                  </a-form-item>
                  <a-form-item label="WWTIOT User Key" class="form-grid-full">
                    <a-input-password v-model="form.wwtiot.user_key" />
                  </a-form-item>
                </div>
              </a-form>
            </section>

            <section v-if="currentStep === 5" class="complete">
              <div class="complete-mark">
                <icon-check-circle />
              </div>
              <h2>{{ t('setup.complete.title') }}</h2>
              <p>{{ t('setup.complete.desc') }}</p>
              <a-button type="primary" size="large" @click="router.replace({ name: 'login' })">
                {{ t('setup.action.login') }}
              </a-button>
            </section>
          </div>

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
      </section>
    </main>
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
    {
      key: 'system',
      title: t('setup.step.system'),
      description: t('setup.step.system.desc'),
      kicker: t('setup.kicker.system'),
      heading: t('setup.heading.system'),
      summary: t('setup.summary.system'),
    },
    {
      key: 'database',
      title: t('setup.step.database'),
      description: t('setup.step.database.desc'),
      kicker: t('setup.kicker.database'),
      heading: t('setup.heading.database'),
      summary: t('setup.summary.database'),
    },
    {
      key: 'redis',
      title: t('setup.step.redis'),
      description: t('setup.step.redis.desc'),
      kicker: t('setup.kicker.redis'),
      heading: t('setup.heading.redis'),
      summary: t('setup.summary.redis'),
    },
    {
      key: 'admin',
      title: t('setup.step.admin'),
      description: t('setup.step.admin.desc'),
      kicker: t('setup.kicker.admin'),
      heading: t('setup.heading.admin'),
      summary: t('setup.summary.admin'),
    },
    {
      key: 'install',
      title: t('setup.step.install'),
      description: t('setup.step.install.desc'),
      kicker: t('setup.kicker.install'),
      heading: t('setup.heading.install'),
      summary: t('setup.summary.install'),
    },
    {
      key: 'complete',
      title: t('setup.step.complete'),
      description: t('setup.step.complete.desc'),
      kicker: t('setup.kicker.complete'),
      heading: t('setup.heading.complete'),
      summary: t('setup.summary.complete'),
    },
  ]);

  const activeStep = computed(() => steps.value[currentStep.value]);

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

  const readinessItems = computed(() => [
    { key: 'database', label: t('setup.signal.database'), ok: dbConnected.value },
    { key: 'redis', label: t('setup.signal.redis'), ok: redisConnected.value },
    { key: 'admin', label: t('setup.signal.admin'), ok: adminValid.value },
    { key: 'runtime', label: t('setup.signal.runtime'), ok: runtimeValid.value },
  ]);

  const readyCount = computed(() => readinessItems.value.filter((item) => item.ok).length);

  const systemCards = computed(() => [
    { key: 'database', icon: 'icon-storage', title: t('setup.card.database.title'), desc: t('setup.card.database.desc') },
    { key: 'cache', icon: 'icon-thunderbolt', title: t('setup.card.redis.title'), desc: t('setup.card.redis.desc') },
    { key: 'admin', icon: 'icon-user', title: t('setup.card.admin.title'), desc: t('setup.card.admin.desc') },
  ]);

  const isStepAvailable = (index: number) => {
    if (index <= currentStep.value) return true;
    if (index === 1) return true;
    if (index === 2) return dbConnected.value;
    if (index === 3) return dbConnected.value && redisConnected.value;
    if (index === 4) return dbConnected.value && redisConnected.value && adminValid.value;
    if (index === 5) return currentStep.value === 5;
    return false;
  };

  const goToStep = (index: number) => {
    if (!isStepAvailable(index)) return;
    errorMessage.value = '';
    currentStep.value = index;
  };

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
    position: relative;
    min-height: 100vh;
    padding: 32px;
    overflow: hidden;
    background: radial-gradient(circle at 14% 18%, rgb(234 182 82 / 18%), transparent 27%),
      radial-gradient(circle at 88% 12%, rgb(90 212 180 / 16%), transparent 30%),
      linear-gradient(135deg, #0f1720, #14232b 50%, #10221f);
  }

  .setup-page-grid {
    position: absolute;
    inset: 0;
    background-image: linear-gradient(rgb(255 255 255 / 4.5%) 1px, transparent 1px),
      linear-gradient(90deg, rgb(255 255 255 / 4.5%) 1px, transparent 1px);
    background-size: 36px 36px;
    mask-image: linear-gradient(to bottom, rgb(0 0 0 / 90%), transparent 86%);
    pointer-events: none;
  }

  .setup-shell {
    position: relative;
    z-index: 1;
    display: grid;
    grid-template-columns: minmax(300px, 348px) minmax(0, 1fr);
    max-width: 1140px;
    min-height: min(760px, calc(100vh - 64px));
    margin: 0 auto;
    overflow: hidden;
    background: #f7f8f2;
    border: 1px solid rgb(255 255 255 / 36%);
    border-radius: 16px;
    box-shadow: 0 28px 78px rgb(0 0 0 / 34%);
  }

  .setup-rail {
    position: relative;
    display: flex;
    flex-direction: column;
    gap: 20px;
    padding: 30px 28px;
    color: #edf4ef;
    background: linear-gradient(160deg, rgb(13 31 39 / 98%), rgb(9 22 27 / 98%)), #0d1d26;

    &::after {
      position: absolute;
      right: 0;
      bottom: 0;
      width: 68%;
      height: 38%;
      background: linear-gradient(135deg, transparent 0 44%, rgb(96 218 184 / 12%) 44% 46%, transparent 46%),
        linear-gradient(45deg, transparent 0 58%, rgb(231 178 75 / 12%) 58% 60%, transparent 60%);
      content: '';
      pointer-events: none;
    }

    &::before {
      position: absolute;
      inset: 0;
      background-image: linear-gradient(rgb(255 255 255 / 4%) 1px, transparent 1px);
      background-size: 100% 48px;
      content: '';
      pointer-events: none;
    }
  }

  .brand-lockup {
    position: relative;
    z-index: 1;
    display: flex;
    gap: 16px;
    align-items: center;

    h1 {
      margin: 4px 0 0;
      color: #fff;
      font-weight: 700;
      font-size: 25px;
      line-height: 31px;
      letter-spacing: 0;
    }
  }

  .brand-mark {
    position: relative;
    display: grid;
    flex: 0 0 52px;
    width: 52px;
    height: 52px;
    place-items: center;
    background: linear-gradient(145deg, #f3c96c, #74dec0);
    border-radius: 14px;
    box-shadow: 0 18px 34px rgb(58 209 172 / 22%);
  }

  .brand-mark-dot {
    width: 14px;
    height: 14px;
    background: #0f2029;
    border-radius: 999px;
    box-shadow: 13px 0 0 #0f2029, 0 13px 0 #0f2029, 13px 13px 0 #0f2029;
  }

  .brand-mark-line {
    position: absolute;
    right: 7px;
    bottom: 7px;
    width: 18px;
    height: 3px;
    background: #0f2029;
    border-radius: 999px;
  }

  .eyebrow,
  .section-kicker {
    margin: 0;
    color: #5fcfb0;
    font-weight: 700;
    font-size: 12px;
    line-height: 18px;
    letter-spacing: 0;
    text-transform: uppercase;
  }

  .setup-subtitle {
    position: relative;
    z-index: 1;
    max-width: 260px;
    margin: 0;
    color: rgb(237 244 239 / 72%);
    line-height: 22px;
  }

  .status-chip {
    position: relative;
    z-index: 1;
    display: inline-flex;
    gap: 8px;
    align-items: center;
    width: fit-content;
    padding: 8px 12px;
    color: #f3c96c;
    font-weight: 600;
    background: rgb(243 201 108 / 10%);
    border: 1px solid rgb(243 201 108 / 28%);
    border-radius: 999px;
  }

  .status-chip-ready {
    color: #72dcbc;
    background: rgb(114 220 188 / 10%);
    border-color: rgb(114 220 188 / 30%);
  }

  .status-chip-pulse {
    width: 8px;
    height: 8px;
    background: currentcolor;
    border-radius: 999px;
    box-shadow: 0 0 0 6px rgb(243 201 108 / 12%);
  }

  .setup-timeline {
    position: relative;
    z-index: 1;
    display: grid;
    gap: 8px;
    margin-top: 2px;
  }

  .timeline-step {
    display: grid;
    grid-template-columns: 32px 1fr;
    gap: 11px;
    width: 100%;
    padding: 11px;
    color: rgb(237 244 239 / 62%);
    text-align: left;
    background: transparent;
    border: 1px solid transparent;
    border-radius: 10px;
    cursor: pointer;
    transition: background 0.2s ease, border-color 0.2s ease, transform 0.2s ease;

    strong,
    small {
      display: block;
      letter-spacing: 0;
    }

    strong {
      color: inherit;
      font-size: 14px;
      line-height: 20px;
    }

    small {
      margin-top: 2px;
      color: rgb(237 244 239 / 48%);
      font-size: 12px;
      line-height: 17px;
    }

    &:disabled {
      cursor: not-allowed;
    }

    &:not(:disabled):hover {
      background: rgb(255 255 255 / 7%);
      transform: translateX(2px);
    }
  }

  .timeline-step-active {
    color: #fff;
    background: rgb(255 255 255 / 10%);
    border-color: rgb(114 220 188 / 34%);
  }

  .timeline-step-done {
    color: #7de0c1;
  }

  .timeline-step-locked {
    opacity: 0.48;
  }

  .timeline-step-index {
    display: grid;
    width: 32px;
    height: 32px;
    color: #102029;
    font-weight: 800;
    background: rgb(237 244 239 / 84%);
    border-radius: 9px;
    place-items: center;
  }

  .timeline-step-active .timeline-step-index,
  .timeline-step-done .timeline-step-index {
    background: linear-gradient(145deg, #f3c96c, #70dcbc);
  }

  .setup-workbench {
    display: flex;
    flex-direction: column;
    min-width: 0;
    padding: 30px;
    background: linear-gradient(180deg, rgb(255 255 255 / 74%), rgb(247 248 242 / 98%)), #f7f8f2;
  }

  .workbench-header {
    display: flex;
    gap: 24px;
    align-items: flex-start;
    justify-content: space-between;
    padding-bottom: 20px;
    border-bottom: 1px solid rgb(16 32 41 / 8%);

    h2 {
      margin: 4px 0 8px;
      color: #16232a;
      font-weight: 750;
      font-size: 28px;
      line-height: 36px;
      letter-spacing: 0;
    }

    p {
      max-width: 560px;
      margin: 0;
      color: #617079;
      line-height: 22px;
    }
  }

  .step-meter {
    display: flex;
    flex: 0 0 auto;
    align-items: baseline;
    padding: 10px 14px;
    color: #617079;
    background: #fff;
    border: 1px solid rgb(16 32 41 / 8%);
    border-radius: 12px;

    span {
      color: #16232a;
      font-weight: 800;
      font-size: 26px;
      line-height: 1;
    }

    small {
      margin-left: 3px;
    }
  }

  .setup-error {
    margin-top: 18px;
  }

  .setup-card {
    display: flex;
    flex: 1 1 auto;
    flex-direction: column;
    min-height: 0;
    margin-top: 18px;
    overflow: hidden;
    background: linear-gradient(180deg, #fff, #fbfcf7);
    border: 1px solid rgb(16 32 41 / 10%);
    border-radius: 14px;
    box-shadow: 0 18px 44px rgb(16 32 41 / 8%);
  }

  .console-topline {
    display: flex;
    gap: 16px;
    align-items: center;
    justify-content: space-between;
    padding: 16px 18px;
    background: #101f28;
    border-bottom: 1px solid rgb(255 255 255 / 9%);
  }

  .console-title {
    display: inline-flex;
    gap: 9px;
    align-items: center;
    min-width: 0;
    color: #f5f8ef;

    strong {
      overflow: hidden;
      font-weight: 700;
      white-space: nowrap;
      text-overflow: ellipsis;
    }
  }

  .console-dot {
    width: 9px;
    height: 9px;
    background: #74dec0;
    border-radius: 999px;
    box-shadow: 0 0 0 5px rgb(116 222 192 / 12%);
  }

  .console-count {
    display: inline-flex;
    flex: 0 0 auto;
    gap: 6px;
    align-items: baseline;
    color: rgb(245 248 239 / 58%);
    font-size: 12px;

    strong {
      color: #f3c96c;
      font-size: 18px;
      line-height: 1;
    }
  }

  .readiness-strip {
    display: grid;
    grid-template-columns: repeat(4, minmax(0, 1fr));
    gap: 1px;
    background: rgb(16 32 41 / 8%);
    border-bottom: 1px solid rgb(16 32 41 / 8%);
  }

  .readiness-pill {
    display: inline-flex;
    gap: 7px;
    align-items: center;
    min-width: 0;
    padding: 11px 14px;
    color: #66747b;
    font-size: 12px;
    overflow-wrap: anywhere;
    background: #f4f6ef;

    svg {
      flex: 0 0 auto;
      color: #9aa4a4;
    }
  }

  .readiness-pill-ok {
    color: #0f765b;
    background: #eef8ef;

    svg {
      color: #18a67d;
    }
  }

  .setup-card-body {
    flex: 1 1 auto;
    min-height: 0;
    padding: 20px;
  }

  .preflight-layout {
    display: grid;
    grid-template-columns: minmax(0, 1.08fr) minmax(230px, 0.92fr);
    gap: 16px;
    align-items: stretch;
    height: 100%;
  }

  .terminal-card {
    height: 100%;
    min-height: 260px;
    padding: 18px;
    color: #dfeee9;
    background: linear-gradient(145deg, rgb(13 28 36 / 98%), rgb(18 39 44 / 98%)), #0e1e27;
    border: 1px solid rgb(114 220 188 / 18%);
    border-radius: 14px;
    box-shadow: inset 0 0 0 1px rgb(255 255 255 / 4%);

    dl {
      display: grid;
      gap: 14px;
      margin: 22px 0 0;
    }

    dl div {
      padding-bottom: 14px;
      border-bottom: 1px solid rgb(255 255 255 / 8%);
    }

    dt {
      color: rgb(223 238 233 / 54%);
      font-size: 12px;
      text-transform: uppercase;
    }

    dd {
      margin: 6px 0 0;
      color: #fff;
      font-weight: 650;
      overflow-wrap: anywhere;
    }
  }

  .terminal-card-bar {
    display: flex;
    gap: 7px;

    > span {
      width: 10px;
      height: 10px;
      background: rgb(255 255 255 / 26%);
      border-radius: 999px;

      &:nth-child(1) {
        background: #f3c96c;
      }

      &:nth-child(2) {
        background: #70dcbc;
      }
    }
  }

  .preflight-stack {
    display: grid;
    gap: 10px;
  }

  .preflight-item {
    display: grid;
    grid-template-columns: 40px 1fr;
    gap: 12px;
    padding: 14px;
    background: #f5f7f1;
    border: 1px solid rgb(16 32 41 / 8%);
    border-radius: 10px;

    strong {
      display: block;
      color: #16232a;
      line-height: 20px;
    }

    p {
      margin: 5px 0 0;
      color: #65737a;
      line-height: 20px;
    }
  }

  .preflight-item-icon {
    display: grid;
    width: 40px;
    height: 40px;
    color: #17302f;
    font-size: 20px;
    background: linear-gradient(145deg, rgb(243 201 108 / 80%), rgb(112 220 188 / 80%));
    border-radius: 12px;
    place-items: center;
  }

  .form-stage {
    max-width: none;
  }

  .form-grid {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 2px 18px;
  }

  .form-grid-full {
    grid-column: 1 / -1;
  }

  .inline-status {
    display: flex;
    gap: 16px;
    align-items: center;
    justify-content: space-between;
    padding: 14px;
    background: #f5f7f2;
    border: 1px solid rgb(16 32 41 / 8%);
    border-radius: 12px;

    .password-rule {
      display: inline-flex;
      gap: 8px;
      align-items: center;
      color: #7a8588;
    }

    .ok {
      color: #118263;
    }
  }

  .password-rules {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
    margin-top: 8px;

    .password-rule {
      padding: 6px 10px;
      color: #7a8588;
      font-size: 12px;
      background: #f5f7f2;
      border-radius: 999px;
    }

    .ok {
      color: #0f765b;
      background: rgb(112 220 188 / 18%);
    }
  }

  .mode-switch {
    display: flex;
    gap: 18px;
    align-items: center;
    justify-content: space-between;
    margin: 6px 0 20px;
    padding: 16px;
    background: #f5f7f2;
    border: 1px solid rgb(16 32 41 / 8%);
    border-radius: 12px;

    strong {
      display: block;
      color: #16232a;
    }

    p {
      margin: 4px 0 0;
      color: #65737a;
    }
  }

  .complete {
    display: grid;
    min-height: 360px;
    place-items: center;
    text-align: center;

    h2 {
      margin: 18px 0 8px;
      color: #16232a;
      font-size: 30px;
      line-height: 38px;
    }

    p {
      max-width: 420px;
      margin: 0 0 24px;
      color: #65737a;
      line-height: 22px;
    }
  }

  .complete-mark {
    display: grid;
    width: 82px;
    height: 82px;
    color: #113027;
    font-size: 46px;
    background: linear-gradient(145deg, #f3c96c, #70dcbc);
    border-radius: 24px;
    box-shadow: 0 20px 36px rgb(77 185 153 / 24%);
    place-items: center;
  }

  .setup-actions {
    display: flex;
    gap: 12px;
    justify-content: flex-end;
    padding: 15px 18px;
    background: #f4f6ef;
    border-top: 1px solid rgb(16 32 41 / 8%);
  }

  :deep(.arco-input-wrapper),
  :deep(.arco-select-view-single) {
    min-height: 42px;
    background: #fbfcf8;
    border-color: rgb(16 32 41 / 12%);
    border-radius: 10px;
  }

  :deep(.arco-form-item-label-col > label) {
    color: #34454d;
    font-weight: 650;
  }

  :deep(.arco-btn) {
    border-radius: 10px;
  }

  :deep(.arco-btn-primary) {
    color: #102029;
    font-weight: 700;
    background: linear-gradient(145deg, #f3c96c, #70dcbc);
    border: 0;

    &:hover {
      color: #102029;
      background: linear-gradient(145deg, #ffd67a, #7de6c8);
    }

    &[disabled] {
      color: rgb(16 32 41 / 38%);
      background: #d9ddd7;
    }
  }

  @media (width <= 980px) {
    .setup-page {
      padding: 20px;
    }

    .setup-shell {
      grid-template-columns: 1fr;
      min-height: auto;
    }

    .setup-rail {
      padding: 26px;
    }

    .setup-timeline {
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }

    .signal-panel {
      margin-top: 0;
    }

    .preflight-layout {
      grid-template-columns: 1fr;
    }
  }

  @media (width <= 640px) {
    .setup-page {
      padding: 0;
    }

    .setup-shell {
      border-radius: 0;
    }

    .setup-workbench,
    .setup-rail {
      gap: 16px;
      padding: 22px;
    }

    .workbench-header,
    .inline-status,
    .mode-switch {
      flex-direction: column;
      align-items: stretch;
    }

    .brand-lockup {
      gap: 12px;
      align-items: center;

      h1 {
        font-size: 24px;
        line-height: 30px;
      }
    }

    .brand-mark {
      flex-basis: 46px;
      width: 46px;
      height: 46px;
      border-radius: 13px;
    }

    .setup-subtitle {
      max-width: none;
    }

    .setup-timeline,
    .signal-list,
    .form-grid {
      grid-template-columns: 1fr;
    }

    .setup-timeline {
      grid-template-columns: repeat(3, minmax(0, 1fr));
      gap: 8px;
    }

    .timeline-step {
      display: flex;
      flex-direction: column;
      gap: 8px;
      min-height: 72px;
      padding: 9px;

      small {
        display: none;
      }

      strong {
        font-size: 13px;
      }
    }

    .timeline-step-index {
      width: 30px;
      height: 30px;
      border-radius: 9px;
    }

    .signal-panel {
      display: none;
    }

    .status-chip {
      display: none;
    }

    .setup-card {
      padding: 18px;
    }

    .readiness-strip {
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }

    .readiness-pill {
      padding: 10px 12px;
    }

    .workbench-header h2 {
      font-size: 24px;
      line-height: 31px;
    }

    .setup-actions {
      display: grid;
      grid-template-columns: 1fr 1fr;
    }
  }
</style>
