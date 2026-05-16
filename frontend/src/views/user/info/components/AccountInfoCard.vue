<template>
  <a-card v-bind="{ ...attrs }">
    <div class="account-summary">
      <a-avatar :size="72" class="account-avatar">{{ avatarInitial }}</a-avatar>
      <div>
        <div class="account-name">{{ displayName }}</div>
        <div class="account-email">{{ userInfo.email || '-' }}</div>
      </div>
    </div>

    <a-descriptions :column="1" bordered class="account-descriptions">
      <a-descriptions-item :label="t('userInfo.account.id')">
        <a-typography-paragraph class="!m-0" copyable>{{ userInfo.id || '-' }}</a-typography-paragraph>
      </a-descriptions-item>
      <a-descriptions-item :label="t('userInfo.account.displayName')">
        {{ displayName }}
      </a-descriptions-item>
      <a-descriptions-item :label="t('userInfo.account.email')">
        {{ userInfo.email || '-' }}
      </a-descriptions-item>
      <a-descriptions-item :label="t('userInfo.account.emailVerified')">
        <a-tag :color="userInfo.email_verified ? 'green' : 'orange'">
          {{ userInfo.email_verified ? t('userInfo.account.verified') : t('userInfo.account.unverified') }}
        </a-tag>
      </a-descriptions-item>
      <a-descriptions-item :label="t('userInfo.account.roles')">
        <a-space wrap>
          <a-tag v-for="role in userInfo.roles" :key="role" color="arcoblue">{{ role }}</a-tag>
          <span v-if="!userInfo.roles.length">-</span>
        </a-space>
      </a-descriptions-item>
    </a-descriptions>

    <a-alert type="info" show-icon class="password-notice">
      {{ t('userInfo.account.passwordNotice') }}
    </a-alert>
  </a-card>
</template>

<script lang="ts" setup>
  import { computed, useAttrs } from 'vue';
  import { useI18n } from 'vue-i18n';
  import { useUserStore } from '@/store';

  const { t } = useI18n();
  const attrs = useAttrs();

  const userStore = useUserStore();
  const userInfo = computed(() => userStore.userInfo);
  const displayName = computed(() => userInfo.value.nickname || userInfo.value.name || userInfo.value.email || '-');
  const avatarInitial = computed(() => displayName.value.slice(0, 1).toUpperCase());
</script>

<style scoped lang="less">
  .account-summary {
    display: flex;
    gap: 16px;
    align-items: center;
    margin-bottom: 20px;
  }

  .account-avatar {
    color: #fff;
    font-weight: 600;
    background: rgb(var(--primary-6));
  }

  .account-name {
    color: var(--color-text-1);
    font-weight: 600;
    font-size: 18px;
    line-height: 1.4;
  }

  .account-email {
    margin-top: 4px;
    color: var(--color-text-3);
    font-size: 14px;
  }

  .account-descriptions {
    margin-top: 8px;
  }

  .password-notice {
    margin-top: 16px;
  }
</style>
