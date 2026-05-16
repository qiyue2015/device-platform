<template>
  <div class="password-help">
    <a-alert type="info" show-icon>
      {{ t('auth.passwordRecovery.adminNotice') }}
    </a-alert>
    <a-button type="primary" long @click="$router.push({ name: 'login' })">{{ t('auth.forgot.backToLogin') }}</a-button>
  </div>
</template>

<script lang="ts" setup>
  import { useI18n } from 'vue-i18n';

  const { t } = useI18n();
</script>

<style lang="less" scoped>
  .password-help {
    display: flex;
    flex-direction: column;
    gap: 16px;
  }
</style>
