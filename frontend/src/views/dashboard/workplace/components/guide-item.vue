<template>
  <div class="guide-step-item">
    <div class="guide-step-item-title">
      <span class="guide-step-item-title-index">{{ step }}</span>
      <span class="guide-step-item-title-text">{{ title }}</span>
    </div>
    <div class="guide-step-item-introduction">
      <slot />
    </div>
    <div class="guide-step-item-content">
      <slot name="content" />
    </div>
  </div>
</template>

<script lang="ts" setup>
  defineProps({
    step: {
      type: String,
      default: '01',
    },
    title: {
      type: String,
      default: '',
    },
  });
</script>

<style lang="less" scoped>
  .guide-step-item {
    flex: 1;
    box-sizing: border-box;
    padding: 20px 24px;
    background: var(--color-fill-1);

    &-title {
      line-height: 24px;

      &-index {
        display: inline-block;
        margin-right: 4px;
        color: var(--color-text-1);
        font-weight: 600;
        font-size: 20px;
        line-height: 23px;
      }

      &-text {
        color: var(--color-text-1);
        font-weight: 500;
        font-size: 16px;
        line-height: 24px;
      }
    }

    &-introduction {
      margin: 12px 0;
      color: var(--color-text-2);
      font-weight: 400;
      font-size: 12px;
      font-style: normal;
      line-height: 20px;
    }
  }
</style>
