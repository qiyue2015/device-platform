<template>
  <div class="page-container">
    <AccountInfoCard :title="$t('userInfo.title.accountInfo')" class="general-card" />
  </div>
</template>

<script lang="ts" setup>
  import AccountInfoCard from './components/AccountInfoCard.vue';

  defineOptions({ name: 'UserInfo' });
</script>

<style scoped lang="less"></style>
