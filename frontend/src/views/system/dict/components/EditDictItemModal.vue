<template>
  <a-modal
    v-model:visible="visible"
    :title="isEdit ? $t('system.dict.editItemModal.titleEdit') : $t('system.dict.editItemModal.titleCreate')"
    :ok-loading="submitLoading"
    @ok="handleSubmit"
    @close="handleClose"
  >
    <a-form ref="formRef" :model="formData" auto-label-width>
      <a-form-item :label="$t('system.dict.editItemModal.label')" field="label" :rules="[{ required: true }]">
        <a-input v-model="formData.label" :placeholder="$t('system.dict.editItemModal.label.placeholder')" />
      </a-form-item>
      <a-form-item :label="$t('system.dict.editItemModal.value')" field="value" :rules="[{ required: true }]">
        <a-input v-model="formData.value" :placeholder="$t('system.dict.editItemModal.value.placeholder')" />
      </a-form-item>
      <a-form-item :label="$t('system.dict.editItemModal.sort')" field="sort">
        <a-input-number v-model="formData.sort" :min="0" />
      </a-form-item>
      <a-form-item :label="$t('system.dict.editItemModal.isActive')" field="is_active">
        <a-switch v-model="formData.is_active" />
      </a-form-item>
    </a-form>
  </a-modal>
</template>

<script lang="ts" setup>
  import { ref, reactive } from 'vue';
  import { useI18n } from 'vue-i18n';
  import { Message } from '@arco-design/web-vue';
  import type { FormInstance } from '@arco-design/web-vue';
  import { useVisible } from '@/hooks';
  import { createDictItem, updateDictItem, type DictItemRecord, type DictItemUpdateData } from '@/api/system/dict';

  const props = defineProps<{ typeId?: number }>();
  const emit = defineEmits<{ success: [] }>();
  const { t } = useI18n();
  const { visible, setVisible } = useVisible(false);
  const formRef = ref<FormInstance>();
  const submitLoading = ref(false);
  const isEdit = ref(false);
  const editId = ref<number>();

  const getDefaultForm = (): DictItemUpdateData => ({
    label: '',
    value: '',
    sort: 0,
    is_active: true,
  });

  const formData = reactive(getDefaultForm());

  const resetForm = () => {
    Object.assign(formData, getDefaultForm());
    formRef.value?.resetFields();
  };

  const onCreate = () => {
    isEdit.value = false;
    editId.value = undefined;
    resetForm();
    setVisible(true);
  };

  const onEdit = (record: DictItemRecord) => {
    isEdit.value = true;
    editId.value = record.id;
    Object.assign(formData, {
      label: record.label,
      value: record.value,
      sort: record.sort,
      is_active: record.is_active,
    });
    setVisible(true);
  };

  const handleSubmit = async () => {
    const errors = await formRef.value?.validate();
    if (errors) return;
    submitLoading.value = true;
    try {
      if (isEdit.value && editId.value) {
        await updateDictItem(editId.value, { ...formData });
        Message.success(t('system.dict.editItemModal.updateSuccess'));
      } else if (props.typeId) {
        await createDictItem({
          dictionary_type_id: props.typeId,
          ...formData,
        });
        Message.success(t('system.dict.editItemModal.createSuccess'));
      }
      setVisible(false);
      emit('success');
    } finally {
      submitLoading.value = false;
    }
  };

  const handleClose = () => {
    resetForm();
  };

  defineExpose({ onCreate, onEdit });
</script>
