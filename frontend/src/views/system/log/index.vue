<template>
  <div class="page-container">
    <a-card class="general-card h-full" :title="$t('menu.system.log')">
      <a-tabs v-model:active-key="activeTab">
        <a-tab-pane key="auth" :title="$t('system.log.tab.auth')">
          <AuthLogTable />
        </a-tab-pane>
        <a-tab-pane key="operation" :title="$t('system.log.tab.operation')">
          <OperationLogTable />
        </a-tab-pane>
      </a-tabs>
    </a-card>
  </div>
</template>

<script lang="ts" setup>
  import { ref } from 'vue';
  import AuthLogTable from './components/AuthLogTable.vue';
  import OperationLogTable from './components/OperationLogTable.vue';

  defineOptions({ name: 'SystemLog' });

  const activeTab = ref('auth');
</script>
