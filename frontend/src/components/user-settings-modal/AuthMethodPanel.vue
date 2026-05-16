<template>
  <div>
    <div v-for="(item, index) in authMethods" :key="index" class="auth-item">
      <div class="flex items-center gap-3 flex-1 min-w-0">
        <img :src="item.bound ? item.icon : item.unbindIcon" :alt="item.name" class="auth-icon" />
        <div class="min-w-0">
          <div class="flex items-center gap-2">
            <span class="font-medium">{{ item.name }}</span>
            <a-badge
              v-if="item.bound"
              status="success"
              :text="item.type === 'password' ? t('common.status.set') : t('common.status.bound')"
              :style="{ '--color-text-1': 'rgb(var(--success-6))' }"
            />
            <a-badge
              v-else
              status="warning"
              :text="item.type === 'password' ? t('common.status.notSet') : t('common.status.notBound')"
              :style="{ '--color-text-1': 'rgb(var(--warning-6))' }"
            />
          </div>
          <div class="text-sm text-gray-500 mt-1 truncate">{{ item.description }}</div>
        </div>
      </div>
      <div class="flex-shrink-0">
        <template v-if="item.type === 'password'">
          <a-space>
            <a-button type="primary" status="normal" size="small" @click="onAction(item)">
              {{ t('common.action.modify') }}
            </a-button>
            <a-button size="small" @click="onForgotPassword">
              {{ t('userSettings.profile.forgotPassword') }}
            </a-button>
          </a-space>
        </template>
        <template v-else-if="item.type === 'phone' || item.type === 'email'">
          <a-button type="primary" status="normal" size="small" @click="onAction(item)">
            {{ item.bound ? t('common.action.unbind') : t('common.action.bind') }}
          </a-button>
        </template>
        <template v-else>
          <a-button :type="item.bound ? 'secondary' : 'primary'" status="normal" size="small" @click="onAction(item)">
            {{ item.bound ? t('common.action.unbind') : t('common.action.bind') }}
          </a-button>
        </template>
      </div>
    </div>
    <EditPasswordModal ref="editPasswordRef" />
    <EditPhoneModal ref="editPhoneRef" />
    <EditEmailModal ref="editEmailRef" />
  </div>
</template>

<script setup lang="ts">
  import { computed, ref } from 'vue';
  import { useRouter } from 'vue-router';
  import { Message } from '@arco-design/web-vue';
  import { useI18n } from 'vue-i18n';
  import { useUserStore } from '@/store';
  import UserIcon from './icons/user.svg?url';
  import UnbindUserIcon from './icons/user-unbind.svg?url';
  import MailIcon from './icons/mail.svg?url';
  import MailIconUnbind from './icons/mail-unbind.svg?url';
  import PhoneIcon from './icons/tel.svg?url';
  import PhoneIconUnbind from './icons/tel-unbind.svg?url';
  import WechatIcon from './icons/wechat.svg?url';
  import WechatIconUnbind from './icons/wechat-unbind.svg?url';
  import GoogleIcon from './icons/google.svg?url';
  import GoogleIconUnbind from './icons/google-unbind.svg?url';
  import EditPasswordModal from './EditPasswordModal.vue';
  import EditPhoneModal from './EditPhoneModal.vue';
  import EditEmailModal from './EditEmailModal.vue';

  const { t } = useI18n();
  const router = useRouter();
  const userStore = useUserStore();
  const userInfo = computed(() => userStore.userInfo);

  const authMethods = computed(() => [
    {
      type: 'password',
      icon: UserIcon,
      unbindIcon: UnbindUserIcon,
      bound: !!userInfo.value.name,
      name: t('userInfo.auth.password.name'),
      description: t('userInfo.auth.password.desc'),
    },
    {
      type: 'phone',
      icon: PhoneIcon,
      unbindIcon: PhoneIconUnbind,
      bound: !!userInfo.value.mobile,
      name: t('userInfo.auth.phone.name'),
      description: t('userInfo.auth.phone.desc'),
    },
    {
      type: 'email',
      icon: MailIcon,
      unbindIcon: MailIconUnbind,
      bound: !!userInfo.value.email,
      name: t('userInfo.auth.email.name'),
      description: t('userInfo.auth.email.desc'),
    },
    {
      type: 'google',
      icon: GoogleIcon,
      unbindIcon: GoogleIconUnbind,
      bound: false,
      name: t('userInfo.auth.google.name'),
      description: t('userInfo.auth.google.desc'),
    },
    {
      type: 'wechat',
      icon: WechatIcon,
      unbindIcon: WechatIconUnbind,
      bound: false,
      name: t('userInfo.auth.wechat.name'),
      description: t('userInfo.auth.wechat.desc'),
    },
  ]);

  const editPasswordRef = ref<InstanceType<typeof EditPasswordModal>>();
  const editPhoneRef = ref<InstanceType<typeof EditPhoneModal>>();
  const editEmailRef = ref<InstanceType<typeof EditEmailModal>>();

  const onForgotPassword = () => router.push({ name: 'forgot-password' });

  const onAction = (item: any) => {
    if (item.type === 'password') {
      editPasswordRef.value?.onEdit();
    } else if (item.type === 'phone') {
      editPhoneRef.value?.onEdit();
    } else if (item.type === 'email') {
      editEmailRef.value?.onEdit();
    } else {
      Message.info(t('userInfo.auth.redirectAuth'));
    }
  };
</script>

<style scoped lang="less">
  .auth-item {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 14px 0;
    border-bottom: 1px solid var(--color-fill-2);

    &:last-child {
      border-bottom: none;
    }
  }

  .auth-icon {
    flex-shrink: 0;
    width: 32px;
    height: 32px;
  }
</style>
