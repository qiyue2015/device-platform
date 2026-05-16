<template>
  <div class="brand-banner-container">
    <div class="brand-banner-inner">
      <div class="brand-banner-inner-content">
        <div class="brand-banner-inner-content-title">{{ title }}</div>
        <div class="brand-banner-inner-content-description">
          <template v-if="description">{{ description }}</template>
          <slot v-else />
        </div>
      </div>
      <div v-if="thumb" class="brand-banner-inner-thumb">
        <img :src="thumb" />
      </div>
    </div>
  </div>
</template>

<script lang="ts" setup>
  defineProps({
    title: {
      type: String,
      default: '',
    },
    description: {
      type: String,
      default: '',
    },
    thumb: {
      type: String,
      default: '',
    },
  });
</script>

<style lang="less" scoped>
  .brand-banner {
    &-container {
      width: 100%;
      min-height: 220px;
    }

    &-inner {
      position: relative;
      display: flex;
      align-items: flex-start;

      &-content {
        flex: 1;
        margin-right: 540px;
        padding-top: 16px;
        overflow: hidden;

        &-title {
          color: var(--color-text-1);
          font-weight: 500;
          font-size: 30px;
          line-height: 38px;
        }

        &-description {
          margin-top: 16px;
          color: var(--color-text-2);
          font-size: 13px;
          line-height: 22px;
        }

        &-operation {
          margin-top: 24px;
        }
      }

      &-thumb {
        position: absolute;
        top: 0;
        right: 0;
        z-index: 2;
        height: 220px;

        img {
          width: 480px;
          height: 220px;
        }
      }
    }
  }
</style>
