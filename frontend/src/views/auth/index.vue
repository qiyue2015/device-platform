<template>
  <div class="auth-container">
    <div class="logo">
      <div class="logo-text">{{ appStore?.app_name }}</div>
    </div>
    <div class="content">
      <div class="content-inner flex flex-col gap-4">
        <div class="auth-title text-2xl font-brand mb-1">{{ pageTitle }}</div>
        <div class="auth-subtitle">{{ pageSubtitle }}</div>
        <PasswordLoginForm v-if="$route.name === 'login'" />
        <ForgotPasswordForm v-else-if="$route.name === 'forgot-password'" />
        <ResetPasswordForm v-else-if="$route.name === 'reset-password'" />
      </div>
    </div>
    <div class="footer">
      <Footer />
    </div>
  </div>
</template>

<script lang="ts" setup>
  import { computed } from 'vue';
  import { useAppStore } from '@/store';
  import { useDark } from '@vueuse/core';
  import Footer from '@/components/footer/index.vue';
  import { useRoute } from 'vue-router';
  import { useI18n } from 'vue-i18n';
  import PasswordLoginForm from './components/PasswordLoginForm.vue';
  import ForgotPasswordForm from './components/ForgotPasswordForm.vue';
  import ResetPasswordForm from './components/ResetPasswordForm.vue';

  const { t } = useI18n();
  const appStore = useAppStore();
  const route = useRoute();
  const pageTitle = computed(() => {
    if (route.name === 'forgot-password') return t('auth.forgot.title');
    if (route.name === 'reset-password') return t('auth.reset.title');
    return t('auth.welcome', { appName: appStore?.app_name });
  });
  const pageSubtitle = computed(() => {
    if (route.name === 'forgot-password') return t('auth.forgot.desc');
    if (route.name === 'reset-password') return t('auth.reset.desc');
    return t('auth.login.desc');
  });

  useDark({
    selector: 'body',
    attribute: 'arco-theme',
    valueDark: 'dark',
    valueLight: 'light',
    storageKey: 'arco-theme',
    onChanged(dark: boolean) {
      appStore.toggleTheme(dark);
    },
  });
</script>

<style lang="less" scoped>
  .auth-container {
    @apply ~"flex flex-col justify-between w-full h-screen p-4 gap-4";

    background-image: url('assets/images/login-bg.png');
    background-repeat: no-repeat;
    background-position: 50%;
    background-size: cover;

    .logo {
      @apply ~"flex items-center gap-2 mb-4 hidden sm:flex";

      &-text {
        margin-right: 4px;
        margin-left: 4px;
        color: var(--color-text-1);
        font-size: 20px;
        font-family: '钉钉进步体 Regular', sans-serif;
      }
    }

    .content {
      @apply ~"relative flex flex-1 items-center justify-center";

      &-inner {
        @apply w-full max-w-md p-6 lg:p-10;

        overflow: hidden;
        background: var(--color-bg-4);
        border-radius: 12px;
      }
    }

    .footer {
      width: 100%;
    }
  }

  body[arco-theme='dark'] {
    .auth-container {
      background: var(--color-bg-1);
    }
  }

  .auth-title {
    color: var(--color-text-1);
  }

  .auth-subtitle {
    margin-bottom: 12px;
    color: var(--color-text-3);
    font-size: 14px;
    line-height: 1.6;
  }
</style>
