<template>
  <div v-if="items.length" class="product-advance">
    <div v-for="(row, index) in items" :key="index" class="item">
      <div class="title">{{ row.title }}</div>
      <div class="description">{{ row.description }}</div>
    </div>
  </div>
</template>

<script lang="ts" setup>
  defineProps<{
    items: Array<{ title: string; description: string }>;
  }>();
</script>

<style lang="less" scoped>
  .product-advance {
    display: flex;
    flex-wrap: wrap;
    gap: 16px;

    .item {
      width: calc(50% - 8px);
      padding: 12px 16px;
      background-color: var(--color-fill-1);

      .title {
        margin-bottom: 8px;
        color: var(--color-text-1);
        font-weight: 500;
        font-size: 16px;
        line-height: 24px;
      }

      .description {
        color: var(--color-text-3);
        font-size: 13px;
        line-height: 22px;
      }
    }
  }
</style>
