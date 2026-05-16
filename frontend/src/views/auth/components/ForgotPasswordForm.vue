<template>
  <div>
    <div class="mb-4">
      <a-typography-text type="secondary">{{ t('auth.forgot.desc') }}</a-typography-text>
    </div>
    <a-form ref="formRef" :model="formData" :rules="rules" layout="vertical" size="large" @submit-success="handleSubmit">
      <a-form-item field="email" :validate-trigger="['change', 'blur']" required hide-label>
        <a-input v-model="formData.email" :placeholder="t('auth.login.email.placeholder')" allow-clear />
      </a-form-item>
      <a-form-item hide-label>
        <a-button type="primary" html-type="submit" long :loading="loading">
          {{ t('auth.forgot.submit') }}
        </a-button>
      </a-form-item>
    </a-form>
    <div class="text-center text-sm mt-4">
      <a-link class="text-sm" @click="$router.push({ name: 'login' })">{{ t('auth.forgot.backToLogin') }}</a-link>
    </div>
  </div>
</template>

<script lang="ts" setup>
  import { ref, reactive } from 'vue';
  import { useI18n } from 'vue-i18n';
  import { Message } from '@arco-design/web-vue';
  import useLoading from '@/hooks/loading';
  import { forgotPassword } from '@/api/user';

  const { t } = useI18n();
  const { loading, setLoading } = useLoading();

  const formData = reactive({ email: '' });
  const formRef = ref();

  const rules = {
    email: [
      { required: true, message: t('auth.login.email.required') },
      { type: 'email', message: t('auth.login.email.invalid') },
    ],
  };

  const handleSubmit = async () => {
    try {
      setLoading(true);
      await forgotPassword({ email: formData.email });
      Message.success(t('auth.forgot.success'));
    } catch (err) {
      formRef.value.setFields({
        email: { status: 'error', message: (err as Error).message },
      });
    } finally {
      setLoading(false);
    }
  };
</script>
