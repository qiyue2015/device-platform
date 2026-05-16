<template>
  <a-config-provider :locale="locale">
    <div v-if="!isFontLoaded" class="font-loading">
      <a-spin dot />
    </div>
    <router-view v-else />
    <global-setting />
  </a-config-provider>
</template>

<script lang="ts" setup>
  import { computed, onMounted, ref } from 'vue';
  import enUS from '@arco-design/web-vue/es/locale/lang/en-us';
  import zhCN from '@arco-design/web-vue/es/locale/lang/zh-cn';
  import GlobalSetting from '@/components/global-setting/index.vue';
  import useLocale from '@/hooks/locale';
  import DingTalkJinBuTiWoff2 from '@/assets/webfont/DingTalk-JinBuTi.woff2';
  import DingTalkJinBuTiWoff from '@/assets/webfont/DingTalk-JinBuTi.woff';

  const { currentLocale } = useLocale();
  const locale = computed(() => {
    switch (currentLocale.value) {
      case 'zh-CN':
        return zhCN;
      case 'en-US':
        return enUS;
      default:
        return zhCN;
    }
  });

  const isFontLoaded = ref(false);

  onMounted(() => {
    const font = new FontFace(
      '钉钉进步体 Regular',
      `url(${DingTalkJinBuTiWoff2}) format('woff2'), url(${DingTalkJinBuTiWoff}) format('woff')`,
      {
        weight: '400',
        display: 'swap',
      }
    );
    font
      .load()
      .then((loadedFont) => {
        (document.fonts as any).add(loadedFont);
        isFontLoaded.value = true;
      })
      .catch(() => {
        isFontLoaded.value = true;
      });
  });
</script>

<style scoped lang="less">
  .font-loading {
    display: flex;
    align-items: center;
    justify-content: center;
    height: 100vh;
  }
</style>
