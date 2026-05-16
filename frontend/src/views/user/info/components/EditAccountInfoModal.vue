<template>
  <a-modal
    v-model:visible="visible"
    :title="t('userInfo.editInfo.title')"
    title-align="start"
    @before-ok="onSave"
    @close="onReset"
  >
    <a-form ref="formRef" :rules="formRules" :model="formData" layout="vertical">
      <a-form-item :label="t('userInfo.editInfo.username')" field="username" asterisk-position="end">
        <a-input
          v-model="formData.username"
          :placeholder="t('userInfo.editInfo.username.placeholder')"
          :max-length="20"
          show-word-limit
        >
          <template #prefix>@</template>
        </a-input>
      </a-form-item>
      <a-form-item :label="t('userInfo.editInfo.nickname')" field="nickname" asterisk-position="end">
        <a-input
          v-model="formData.nickname"
          :placeholder="t('userInfo.editInfo.nickname.placeholder')"
          :max-length="20"
          show-word-limit
        />
      </a-form-item>
      <a-form-item :label="t('userInfo.editInfo.introduction')" field="introduce" asterisk-position="end">
        <a-textarea
          v-model="formData.introduce"
          :max-length="100"
          :placeholder="t('userInfo.editInfo.introduction.placeholder')"
          show-word-limit
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
  import { updateProfile } from '@/api/user';
  import { useUserStore } from '@/store';

  const { t } = useI18n();
  const { visible, setVisible } = useVisible(false);
  const userStore = useUserStore();

  const formRef = ref<FormInstance>();
  const formData = ref({ username: '', nickname: '', introduce: '' });
  const formRules = {
    username: [
      { required: true, message: t('userInfo.editInfo.username.required') },
      { pattern: /^[a-zA-Z0-9_]{1,20}$/, message: t('userInfo.editInfo.username.pattern') },
    ],
    nickname: [{ required: true, message: t('userInfo.editInfo.nickname.required') }],
  };

  const onSave = async () => {
    try {
      if (await formRef.value.validate()) {
        return false;
      }
      await updateProfile({
        name: formData.value.username,
        nickname: formData.value.nickname,
        introduction: formData.value.introduce,
      });
      await userStore.info();
      Message.success(t('userInfo.editInfo.success'));
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
