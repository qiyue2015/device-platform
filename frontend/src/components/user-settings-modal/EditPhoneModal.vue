<template>
  <a-modal v-model:visible="visible" title-align="start" width="460px" @before-ok="onSave" @close="onReset">
    <template #title>{{ t('userInfo.editPhone.title') }}</template>
    <a-form ref="formRef" :rules="formRules" :model="formData" layout="vertical">
      <a-form-item :label="t('userInfo.editPhone.phone')" field="phone" asterisk-position="end">
        <a-input v-model="formData.phone" :placeholder="t('userInfo.editPhone.phone.placeholder')">
          <template #prefix>+86</template>
        </a-input>
      </a-form-item>
      <a-form-item :label="t('userInfo.editPhone.code')" field="code" asterisk-position="end">
        <InputVerifyCode v-model="formData.code" :account="formData.phone" type="phone" />
      </a-form-item>
    </a-form>
  </a-modal>
</template>

<script setup lang="ts">
  import { ref } from 'vue';
  import { FormInstance, Message } from '@arco-design/web-vue';
  import { useI18n } from 'vue-i18n';
  import { useVisible } from '@/hooks';
  import { updatePhone } from '@/api/user';
  import { useUserStore } from '@/store';
  import InputVerifyCode from '@/components/input-verify-code/index.vue';

  const { t } = useI18n();
  const { visible, setVisible } = useVisible(false);
  const userStore = useUserStore();

  const formRef = ref<FormInstance>();
  const formData = ref({ phone: '', code: '' });
  const formRules = {
    phone: [
      { required: true, message: t('userInfo.editPhone.phone.required') },
      {
        validator: (value: string, cb: (msg?: string) => void) => {
          const reg = /^1[3-9]\d{9}$/;
          if (!reg.test(value)) {
            cb(t('userInfo.editPhone.phone.invalid'));
          } else {
            cb();
          }
        },
      },
    ],
    code: [{ required: true, message: t('userInfo.editPhone.code.required') }],
  };

  const onSave = async () => {
    try {
      if (await formRef.value.validate()) {
        return false;
      }
      await updatePhone(formData.value);
      await userStore.info();
      Message.success(t('userInfo.editPhone.success'));
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
