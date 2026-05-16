<template>
  <a-card class="grid-card" v-bind="{ ...attrs }">
    <template v-if="attrs.title" #title>
      <span>{{ attrs.title }}</span>
      <a-tooltip v-if="slots.tooltip" position="right">
        <template #content>
          <slot name="tooltip" />
        </template>
        <icon-question-circle />
      </a-tooltip>
      <a-tooltip v-else-if="attrs.tooltip" position="right">
        <template #content>
          {{ attrs.tooltip }}
        </template>
        <icon-question-circle />
      </a-tooltip>
    </template>
    <!-- default slot -->
    <a-space direction="vertical" size="medium" fill>
      <slot />
    </a-space>
  </a-card>
</template>

<script lang="ts" setup>
  import { useAttrs, useSlots } from 'vue';

  const attrs = useAttrs();
  const slots = useSlots();
</script>

<style lang="less" scoped>
  .grid-card {
    @apply h-full;

    border: none;
    border-radius: 4px;

    & > :deep(.arco-card-header) {
      height: auto;
      padding: 20px;
      border: none;
    }

    & > :deep(.arco-card-body) {
      padding: 0 20px 20px;
    }

    :deep(.arco-card-header-title) {
      .arco-icon {
        margin-left: 8px;
        color: var(--color-text-4);
      }
    }
  }
</style>
