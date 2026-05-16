<template>
  <a-modal
    v-model:visible="visible"
    :title="t('userInfo.editPassword.title')"
    title-align="start"
    @before-ok="onSave"
    @close="onReset"
  >
    <a-form ref="formRef" :rules="formRules" :model="formData" layout="vertical">
      <a-form-item :label="t('userInfo.editPassword.oldPassword')" field="password">
        <a-input-password v-model="formData.password" :placeholder="t('userInfo.editPassword.oldPassword.placeholder')" />
      </a-form-item>
      <a-form-item :label="t('userInfo.editPassword.newPassword')" field="new_password">
        <a-input-password v-model="formData.new_password" :placeholder="t('userInfo.editPassword.newPassword.placeholder')" />
      </a-form-item>
      <a-form-item :label="t('userInfo.editPassword.confirmPassword')" field="confirm_password">
        <a-input-password
          v-model="formData.confirm_password"
          :placeholder="t('userInfo.editPassword.confirmPassword.placeholder')"
        />
      </a-form-item>
    </a-form>
  </a-modal>
</template>

<script setup lang="ts">
  import { ref } from 'vue';
  import { FormInstance, Message } from '@arco-design/web-vue';
  import { useI18n } from 'vue-i18n';
  import { useVisible } from '@/hooks';
  import { changePassword } from '@/api/user';

  const { t } = useI18n();
  const { visible, setVisible } = useVisible(false);

  const formRef = ref<FormInstance>();
  const formData = ref({
    password: '',
    new_password: '',
    confirm_password: '',
  });
  const formRules = {
    password: [{ required: true, message: t('userInfo.editPassword.oldPassword.required') }],
    new_password: [
      {
        required: true,
        message: t('userInfo.editPassword.newPassword.required'),
      },
      {
        validator: (value: string, cb: (msg?: string) => void) => {
          if (!value || value.length < 6 || value.length > 20) {
            cb(t('userInfo.editPassword.newPassword.length'));
            return;
          }
          const arr = [/[a-z]/.test(value), /[A-Z]/.test(value), /\d/.test(value), /[@$!%*?&]/.test(value)];
          const count = arr.filter(Boolean).length;
          if (count < 3) {
            cb(t('userInfo.editPassword.newPassword.complexity'));
          } else {
            cb();
          }
        },
      },
    ],
    confirm_password: [
      {
        required: true,
        message: t('userInfo.editPassword.confirmPassword.required'),
      },
      {
        validator: (value: string, cb: (msg?: string) => void) => {
          if (value !== formData.value.new_password) {
            cb(t('userInfo.editPassword.confirmPassword.mismatch'));
          } else {
            cb();
          }
        },
      },
    ],
  };

  const onSave = async () => {
    try {
      if (await formRef.value.validate()) {
        return false;
      }
      await changePassword({
        password: formData.value.password,
        new_password: formData.value.new_password,
        password_confirmation: formData.value.confirm_password,
      });
      Message.success(t('userInfo.editPassword.success'));
      return true;
    } catch {
      return false;
    }
  };

  const onReset = () => {
    formRef.value?.resetFields();
  };

  const onEdit = async () => setVisible(true);

  defineExpose({ onEdit });
</script>
