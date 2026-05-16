<template>
  <a-table ref="tableRef" row-key="id" v-bind="{ ...attrs }" :bordered="false">
    <template v-for="key in tableSlots" :key="key" #[key]="scoped">
      <slot :key="key" :name="key" v-bind="scoped" />
    </template>
    <template #action="scoped">
      <slot v-if="hasCustomAction" name="action" v-bind="scoped" />
      <a-space v-else fill>
        <a-button size="small" type="text" :disabled="isEditDisabled(scoped.record)" @click="onEdit(scoped.record)">{{
          $t('common.action.edit')
        }}</a-button>
        <a-button
          :loading="scoped.record?.loading"
          size="small"
          type="text"
          :disabled="isDeleteDisabled(scoped.record)"
          @click="onDelete(scoped.record)"
          >{{ $t('common.action.delete') }}</a-button
        >
      </a-space>
    </template>
  </a-table>
</template>

<script lang="ts" setup>
  import { computed, ref, useAttrs, useSlots } from 'vue';
  import { TableData, TableInstance } from '@arco-design/web-vue';

  const props = defineProps<{
    disableEdit?: (record: TableData) => boolean;
    disableDelete?: (record: TableData) => boolean;
  }>();

  const attrs = useAttrs();
  const slots = useSlots();

  const tableRef = ref<TableInstance | null>(null);

  const hasCustomAction = computed(() => !!slots.action);

  const tableSlots = computed(() => {
    return Object.keys(slots).filter((key) => key !== 'action');
  });

  const isEditDisabled = (record: TableData) => {
    return props.disableEdit ? props.disableEdit(record) : false;
  };

  const isDeleteDisabled = (record: TableData) => {
    return props.disableDelete ? props.disableDelete(record) : false;
  };

  const onEdit = (record: TableData) => {
    if (typeof attrs.onEdit === 'function') {
      attrs.onEdit(record);
    }
  };

  const onDelete = (record: TableData) => {
    if (typeof attrs.onDelete === 'function') {
      attrs.onDelete(record);
    }
  };

  defineExpose({ tableRef });
</script>
