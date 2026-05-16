<template>
  <a-modal
    v-model:visible="visible"
    :title="isEdit ? $t('system.dict.editModal.titleEdit') : $t('system.dict.editModal.titleCreate')"
    :ok-loading="submitLoading"
    @ok="handleSubmit"
    @close="handleClose"
  >
    <a-form ref="formRef" :model="formData" auto-label-width>
      <a-form-item :label="$t('system.dict.editModal.name')" field="name" :rules="[{ required: true }]">
        <a-input v-model="formData.name" :placeholder="$t('system.dict.editModal.name.placeholder')" />
      </a-form-item>
      <a-form-item :label="$t('system.dict.editModal.code')" field="code" :rules="[{ required: true }]">
        <a-input v-model="formData.code" :placeholder="$t('system.dict.editModal.code.placeholder')" />
      </a-form-item>
      <a-form-item :label="$t('system.dict.editModal.description')" field="description">
        <a-textarea v-model="formData.description" :placeholder="$t('system.dict.editModal.description.placeholder')" />
      </a-form-item>
      <a-form-item :label="$t('system.dict.editModal.sort')" field="sort">
        <a-input-number v-model="formData.sort" :min="0" />
      </a-form-item>
      <a-form-item :label="$t('system.dict.editModal.isActive')" field="is_active">
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
  import { createDictType, updateDictType, type DictTypeRecord, type DictTypeCreateData } from '@/api/system/dict';

  const emit = defineEmits<{ success: [] }>();
  const { t } = useI18n();
  const { visible, setVisible } = useVisible(false);
  const formRef = ref<FormInstance>();
  const submitLoading = ref(false);
  const isEdit = ref(false);
  const editId = ref<number>();

  const getDefaultForm = (): DictTypeCreateData => ({
    name: '',
    code: '',
    description: '',
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

  const onEdit = (record: DictTypeRecord) => {
    isEdit.value = true;
    editId.value = record.id;
    Object.assign(formData, {
      name: record.name,
      code: record.code,
      description: record.description ?? '',
      sort: record.sort ?? 0,
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
        await updateDictType(editId.value, formData);
        Message.success(t('system.dict.editModal.updateSuccess'));
      } else {
        await createDictType(formData);
        Message.success(t('system.dict.editModal.createSuccess'));
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
