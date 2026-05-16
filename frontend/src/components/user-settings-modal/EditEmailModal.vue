<template>
  <a-modal v-model:visible="visible" title-align="start" width="460px" @before-ok="onSave" @close="onReset">
    <template #title>{{ t('userInfo.editEmail.title') }}</template>
    <a-form ref="formRef" :rules="formRules" :model="formData" layout="vertical">
      <a-form-item :label="t('userInfo.editEmail.email')" field="email" asterisk-position="end">
        <a-input v-model="formData.email" :placeholder="t('userInfo.editEmail.email.placeholder')" />
      </a-form-item>
      <a-form-item :label="t('userInfo.editEmail.code')" field="code" asterisk-position="end">
        <InputVerifyCode v-model="formData.code" :account="formData.email" type="email" />
      </a-form-item>
    </a-form>
  </a-modal>
</template>

<script setup lang="ts">
  import { ref } from 'vue';
  import { FormInstance, Message } from '@arco-design/web-vue';
  import { useI18n } from 'vue-i18n';
  import { useVisible } from '@/hooks';
  import { updateEmail } from '@/api/user';
  import { useUserStore } from '@/store';
  import InputVerifyCode from '@/components/input-verify-code/index.vue';

  const { t } = useI18n();
  const { visible, setVisible } = useVisible(false);
  const userStore = useUserStore();

  const formRef = ref<FormInstance>();
  const formData = ref({ email: '', code: '' });
  const formRules = {
    email: [{ type: 'email', message: t('userInfo.editEmail.email.invalid') }],
    code: [{ required: true, message: t('userInfo.editEmail.code.required') }],
  };

  const onSave = async () => {
    try {
      if (await formRef.value.validate()) {
        return false;
      }
      await updateEmail(formData.value);
      await userStore.info();
      Message.success(t('userInfo.editEmail.success'));
      return true;
    } catch {
      return false;
    }
  };

  const onReset = () => {
    formRef.value?.resetFields();
  };

  const onEdit = async () => {
    setVisible(true);
  };

  defineExpose({ onEdit });
</script>
