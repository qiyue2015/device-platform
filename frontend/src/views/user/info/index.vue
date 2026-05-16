<template>
  <div class="page-container">
    <a-row :gutter="[16, 16]">
      <a-col :span="24">
        <AccountInfoCard :title="$t('userInfo.title.accountInfo')" class="general-card" />
      </a-col>
      <a-col :span="24">
        <a-card :title="$t('userInfo.title.authMethod')" class="general-card flex-1">
          <AuthMethodPanel />
        </a-card>
      </a-col>
    </a-row>
  </div>
</template>

<script lang="ts" setup>
  import AuthMethodPanel from '@/components/user-settings-modal/AuthMethodPanel.vue';
  import AccountInfoCard from './components/AccountInfoCard.vue';

  defineOptions({ name: 'UserInfo' });
</script>

<style scoped lang="less"></style>
