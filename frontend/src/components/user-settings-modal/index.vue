<template>
  <a-modal v-model:visible="visible" :footer="false" hide-title simple modal-class="w-full max-w-6xl overflow-hidden p-0">
    <a-layout>
      <a-layout-sider>
        <div class="text-lg font-semibold tracking-tight p-6">{{ t('userSettings.title') }}</div>
        <a-menu :selected-keys="[activeTab]" @menu-item-click="(key: string) => (activeTab = key)">
          <a-menu-item v-for="tab in tabs" :key="tab.key">{{ tab.label }}</a-menu-item>
        </a-menu>
      </a-layout-sider>
      <a-layout-content class="relative">
        <a-button class="absolute top-4 right-4" type="text" size="small" @click="visible = false">
          <template #icon><icon-close /></template>
        </a-button>
        <div class="w-full max-w-2xl min-h-[50vh] pt-20 pb-12 mx-2 md:mx-auto">
          <ProfilePanel v-if="activeTab === 'profile'" />
          <AuthMethodPanel v-else-if="activeTab === 'authMethod'" />
        </div>
      </a-layout-content>
    </a-layout>
  </a-modal>
</template>

<script setup lang="ts">
  import { ref, computed } from 'vue';
  import { useI18n } from 'vue-i18n';
  import ProfilePanel from './ProfilePanel.vue';
  import AuthMethodPanel from './AuthMethodPanel.vue';

  const { t } = useI18n();
  const visible = ref(false);
  const activeTab = ref('profile');

  const tabs = computed(() => [
    { key: 'profile', label: t('userSettings.tab.profile') },
    { key: 'authMethod', label: t('userSettings.tab.authMethod') },
  ]);

  const open = (tab?: string) => {
    if (tab) activeTab.value = tab;
    visible.value = true;
  };

  defineExpose({ open });
</script>

<style lang="less" scoped>
  :deep(.arco-layout-sider) {
    box-shadow: 1px 0 5px 0 rgb(0 0 0 / 8%);
  }
</style>
