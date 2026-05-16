<template>
  <div class="auth-container">
    <div class="logo">
      <div class="logo-text">{{ appStore?.app_name }}</div>
    </div>
    <div class="content">
      <div class="content-inner flex flex-col gap-4">
        <!-- OIDC 登录失败 -->
        <template v-if="oidcMode && oidcError">
          <div class="flex flex-col items-center justify-center gap-4 py-8">
            <div class="text-red-500 text-center">{{ oidcError }}</div>
            <a-button type="primary" @click="redirectToOidcLogin">{{ $t('auth.reLogin') }}</a-button>
          </div>
        </template>

        <template v-else>
          <div class="auth-title text-2xl font-brand mb-4">{{ $t('auth.welcome', { appName: appStore?.app_name }) }}</div>
          <!-- 登录 -->
          <template v-if="$route.name === 'login'">
            <PasswordLoginForm />
          </template>

          <!-- 注册 -->
          <template v-else-if="$route.name === 'register'">
            <PasswordRegisterForm />
            <div class="text-center text-sm mt-4">
              {{ $t('auth.hasAccount') }} <a-link class="text-sm" @click="onLogin">{{ $t('auth.loginNow') }}</a-link>
            </div>
          </template>

          <!-- 忘记密码 -->
          <template v-else-if="$route.name === 'forgot-password'">
            <ForgotPasswordForm />
          </template>

          <!-- 重置密码 -->
          <template v-else-if="$route.name === 'reset-password'">
            <ResetPasswordForm />
          </template>
        </template>
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
  import { useRouter, useRoute } from 'vue-router';
  import { isOidc, redirectToOidcLogin } from '@/utils/auth-strategy';
  import PasswordLoginForm from './components/PasswordLoginForm.vue';
  import PasswordRegisterForm from './components/PasswordRegisterForm.vue';
  import ForgotPasswordForm from './components/ForgotPasswordForm.vue';
  import ResetPasswordForm from './components/ResetPasswordForm.vue';

  const appStore = useAppStore();
  const route = useRoute();
  const router = useRouter();

  const oidcMode = isOidc();
  const oidcError = computed(() => (route.query.oidcError as string) || '');

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

  const onLogin = () => {
    router.push({ name: 'login' });
  };
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
</style>
