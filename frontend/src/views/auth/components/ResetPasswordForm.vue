<template>
  <div>
    <a-form ref="formRef" :model="formData" :rules="rules" layout="vertical" size="large" @submit-success="handleSubmit">
      <a-form-item field="password" :validate-trigger="['change', 'blur']" required hide-label>
        <a-input-password v-model="formData.password" :placeholder="t('auth.reset.password.placeholder')" />
      </a-form-item>
      <a-form-item field="password_confirmation" :validate-trigger="['change', 'blur']" required hide-label>
        <a-input-password v-model="formData.password_confirmation" :placeholder="t('auth.reset.passwordConfirm.placeholder')" />
      </a-form-item>
      <a-form-item hide-label>
        <a-button type="primary" html-type="submit" long :loading="loading">
          {{ t('auth.reset.submit') }}
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
  import { useRoute, useRouter } from 'vue-router';
  import { useI18n } from 'vue-i18n';
  import { Message } from '@arco-design/web-vue';
  import useLoading from '@/hooks/loading';
  import { resetPassword } from '@/api/user';

  const { t } = useI18n();
  const { loading, setLoading } = useLoading();
  const route = useRoute();
  const router = useRouter();

  const token = route.query.token as string;
  const email = route.query.email as string;

  const formData = reactive({ password: '', password_confirmation: '' });
  const formRef = ref();

  const rules = {
    password: [
      { required: true, message: t('auth.login.password.required') },
      { min: 6, max: 20, message: t('auth.login.password.length') },
    ],
    password_confirmation: [
      { required: true, message: t('auth.reset.passwordConfirm.required') },
      {
        validator: (value: string, cb: (msg?: string) => void) => {
          if (value !== formData.password) cb(t('auth.reset.passwordConfirm.mismatch'));
          else cb();
        },
      },
    ],
  };

  const handleSubmit = async () => {
    try {
      setLoading(true);
      await resetPassword({ token, email, ...formData });
      Message.success(t('auth.reset.success'));
      router.push({ name: 'login' });
    } catch (err) {
      formRef.value.setFields({
        password: { status: 'error', message: (err as Error).message },
      });
    } finally {
      setLoading(false);
    }
  };
</script>
