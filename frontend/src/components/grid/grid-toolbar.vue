<template>
  <div class="grid-toolbar">
    <a-row justify="space-between" align="center">
      <a-space size="medium" fill>
        <slot name="prepend" />
        <a-button v-if="hasCreate" type="primary" @click="onCreate">{{ $t('common.action.create') }}</a-button>
        <slot name="append" />
      </a-space>
      <a-space size="medium" fill>
        <slot name="extra" />
        <a-tooltip v-if="hasRefresh" :content="$t('common.action.refresh')">
          <a-button @click="onRefresh">
            <template #icon><icon-refresh /></template>
          </a-button>
        </a-tooltip>
      </a-space>
    </a-row>
    <slot />
  </div>
</template>

<script lang="ts" setup>
  import { computed, useAttrs } from 'vue';

  const attrs = useAttrs();

  // 检查是否传入了 @create
  const hasCreate = computed(() => 'onCreate' in attrs);

  // 检查是否传入了 @refresh
  const hasRefresh = computed(() => 'onRefresh' in attrs);

  const onCreate = () => {
    if (typeof attrs.onCreate === 'function') {
      attrs.onCreate();
    }
  };

  const onRefresh = () => {
    if (typeof attrs.onRefresh === 'function') {
      attrs.onRefresh();
    }
  };
</script>
