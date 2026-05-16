<template>
  <div class="text-sm">
    <template v-if="type === 'register'">
      <a-space size="mini" wrap fill>
        <a-checkbox :model-value="modelValue" class="text-sm" @change="$emit('update:modelValue', $event)">
          {{ $t('auth.agreement.registerPrefix') }}
        </a-checkbox>
        <a href="/terms-service" class="text-link" target="_blank">{{ $t('auth.agreement.termsOfService') }}</a>
        <a href="/privacy-policy" class="text-link" target="_blank">{{ $t('auth.agreement.privacyPolicy') }}</a>
      </a-space>
    </template>
    <template v-else>
      <span>{{ $t('auth.agreement.loginPrefix') }}</span>
      <a href="/terms-service" class="text-link" target="_blank">{{ $t('auth.agreement.termsOfService') }}</a>
      <a href="/privacy-policy" class="text-link" target="_blank">{{ $t('auth.agreement.privacyPolicy') }}</a>
    </template>
  </div>
</template>

<script lang="ts" setup>
  defineProps<{
    type: 'login' | 'register';
    modelValue?: boolean;
  }>();

  defineEmits<{
    (e: 'update:modelValue', value: boolean): void;
  }>();
</script>
