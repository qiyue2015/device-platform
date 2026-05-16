<template>
  <a-input-group class="w-full">
    <a-input v-bind="{ ...attrs }" :placeholder="$t('common.verifyCode.placeholder')" maxlength="6" />
    <a-button :disabled="!canSendVerifyCode" class="w-36" @click="handleSendVerifyCode">
      {{ sending ? countdown + 's' : $t('common.verifyCode.send') }}
    </a-button>
  </a-input-group>
</template>

<script lang="ts" setup>
  import { Message } from '@arco-design/web-vue';
  import { ref, useAttrs, computed, watch } from 'vue';
  import { useI18n } from 'vue-i18n';

  const { t } = useI18n();
  const attrs = useAttrs();

  const props = defineProps<{
    type: 'phone' | 'email';
    account: string;
  }>();

  const sending = ref(false);
  const countdown = ref(60);
  const interval = ref<ReturnType<typeof setInterval> | null>(null);

  const isValidPhone = (phone: string) => {
    return /^1[3-9]\d{9}$/.test(phone);
  };

  const isValidEmail = (email: string) => {
    return /^[\w.-]+@[\w.-]+\.[a-zA-Z]{2,}$/.test(email);
  };

  const canSendVerifyCode = computed(() => {
    if (props.type === 'phone') {
      return !sending.value && isValidPhone(props.account);
    }
    if (props.type === 'email') {
      return !sending.value && isValidEmail(props.account);
    }
    return false;
  });

  const handleSendVerifyCode = () => {
    if (!props.account) {
      Message.error(props.type === 'phone' ? t('common.verifyCode.inputPhone') : t('common.verifyCode.inputEmail'));
      return;
    }
    if (props.type === 'phone' && !isValidPhone(props.account)) {
      Message.error(t('common.verifyCode.invalidPhone'));
      return;
    }
    if (props.type === 'email' && !isValidEmail(props.account)) {
      Message.error(t('common.verifyCode.invalidEmail'));
      return;
    }
    if (sending.value) {
      return;
    }

    sending.value = true;
    countdown.value = 60;

    if (interval.value) {
      clearInterval(interval.value);
    }
    interval.value = setInterval(() => {
      if (countdown.value > 0) {
        countdown.value -= 1;
      } else {
        if (interval.value) clearInterval(interval.value);
        sending.value = false;
        interval.value = null;
      }
    }, 1000);
  };

  // 监听弹窗关闭时重置倒计时
  watch(
    () => props.account,
    (newVal) => {
      if (!newVal && sending.value) {
        // 账号被清空，重置倒计时
        if (interval.value) {
          clearInterval(interval.value);
          interval.value = null;
        }
        sending.value = false;
        countdown.value = 60;
      }
    }
  );
</script>
