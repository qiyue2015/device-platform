<template>
  <div class="login-container">
    <a-form ref="registerForm" :model="userInfo" :rules="rules" layout="vertical" size="large" @submit-success="handleSubmit">
      <a-form-item field="email" :validate-trigger="['change', 'blur']" hide-label>
        <a-input v-model="userInfo.email" :placeholder="t('auth.register.email.placeholder')" allow-clear />
      </a-form-item>
      <a-form-item field="username" :validate-trigger="['change', 'blur']" hide-label>
        <a-input v-model="userInfo.username" :placeholder="t('auth.register.username.placeholder')" allow-clear />
      </a-form-item>
      <a-form-item field="password" :validate-trigger="['change', 'blur']" hide-label>
        <a-input
          v-model="userInfo.password"
          type="password"
          :placeholder="t('auth.register.password.placeholder')"
          allow-clear
        />
      </a-form-item>
      <a-form-item hide-label>
        <AgreementNotice v-model="agreed" type="register" />
      </a-form-item>
      <a-button type="primary" size="large" html-type="submit" long :loading="loading" :disabled="!agreed">
        {{ t('auth.startExperience') }}
      </a-button>
    </a-form>
  </div>
</template>

<script lang="ts" setup>
  import { ref, reactive } from 'vue';
  import { useI18n } from 'vue-i18n';
  import { useLoading } from '@/hooks';
  import { useUserStore } from '@/store';
  import { useRouter } from 'vue-router';
  import { Message } from '@arco-design/web-vue';
  import { DEFAULT_ROUTE_NAME } from '@/router/constants';
  import AgreementNotice from './AgreementNotice.vue';

  const { t } = useI18n();
  const userStore = useUserStore();
  const router = useRouter();

  const agreed = ref(false);

  const userInfo = reactive({
    email: '',
    username: '',
    password: '',
  });

  const rules = {
    email: [
      { required: true, message: t('auth.register.email.required') },
      { type: 'email', message: t('auth.register.email.invalid') },
    ],
    username: [
      { required: true, message: t('auth.register.username.required') },
      { min: 3, max: 20, message: t('auth.register.username.length') },
    ],
    password: [
      { required: true, message: t('auth.register.password.required') },
      { min: 6, max: 20, message: t('auth.register.password.length') },
    ],
  };

  const { loading, setLoading } = useLoading();
  const registerForm = ref();
  const handleSubmit = async (values: Record<string, any>) => {
    try {
      setLoading(true);
      values.password_confirmation = values.password;
      await userStore.register(values as any);
      const { redirect, ...othersQuery } = router.currentRoute.value.query;
      router.push({
        name: (redirect as string) || DEFAULT_ROUTE_NAME,
        query: { ...othersQuery },
      });
      Message.success(t('login.form.register.success'));
    } catch (err) {
      registerForm.value.setFields({
        password: {
          status: 'error',
          message: (err as Error).message,
        },
      });
    } finally {
      setLoading(false);
    }
  };
</script>

<style lang="less" scoped></style>
