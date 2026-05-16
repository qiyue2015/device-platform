<template>
  <a-layout>
    <!-- <a-layout-header>
      <div class="navbar"></div>
    </a-layout-header> -->
    <a-layout-content>
      <div style="height: 100vh">
        <iframe :id="iframeId" class="ComfyUI-iframe" :src="comfyCloudUrl" frameborder="0" title="comfyUI" />
      </div>
    </a-layout-content>
  </a-layout>
</template>

<script lang="ts" setup>
  import { useAppStore } from '@/store';
  import { useDark } from '@vueuse/core';
  import { computed } from 'vue';
  import { useRoute } from 'vue-router';

  const appStore = useAppStore();
  const route = useRoute();

  const iframeId = computed(() => `iframe${route.params.id}`);
  const comfyCloudUrl = computed(() => {
    return `${import.meta.env.VITE_COMFY_CLOUD_URL}#workflow_id=${route.params.id}`;
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
  .navbar {
    display: flex;
    justify-content: space-between;
    height: 100%;
    height: 64px;
    background-color: var(--color-bg-2);
    border-bottom: 1px solid var(--color-border);
  }

  .ComfyUI-iframe {
    width: 100%;
    height: 100%;
    border: none;
  }
</style>
