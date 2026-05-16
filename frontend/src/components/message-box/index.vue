<template>
  <a-spin style="display: block" :loading="loading">
    <div class="message-box">
      <div class="message-box-header">
        <span class="header-title"> {{ $t('messageBox.tab.title.notice') }}{{ unreadCountText }} </span>
        <a-button type="text" size="small" @click="handleClear">
          {{ $t('messageBox.tab.button') }}
        </a-button>
      </div>
      <div class="message-box-body">
        <a-result v-if="!notificationList.length" status="404">
          <template #subtitle>{{ $t('messageBox.noContent') }}</template>
        </a-result>
        <List v-else :render-list="notificationList" :unread-count="unreadCount" @item-click="handleItemClick" />
      </div>
      <div class="message-box-footer">
        <div class="footer-item">
          <a-link @click="handleAllRead">{{ $t('messageBox.allRead') }}</a-link>
        </div>
        <div class="footer-item">
          <a-link @click="handleViewMore">{{ $t('messageBox.viewMore') }}</a-link>
        </div>
      </div>
    </div>
  </a-spin>
</template>

<script lang="ts" setup>
  import { ref, computed } from 'vue';
  import { useRouter } from 'vue-router';
  import {
    queryNotificationList,
    markNotificationRead,
    markAllNotificationsRead,
    clearAllNotifications,
    NotificationRecord,
    MessageRecord,
  } from '@/api/message';
  import useLoading from '@/hooks/loading';
  import List from './list.vue';

  const { loading, setLoading } = useLoading(true);
  const router = useRouter();
  const notificationList = ref<MessageRecord[]>([]);
  function mapNotification(n: NotificationRecord): MessageRecord {
    return {
      id: Number(n.id) || 0,
      type: n.type || 'notice',
      title: n.title || '',
      subTitle: '',
      content: n.content || '',
      time: n.created_at || '',
      status: n.read_at ? 1 : 0,
    };
  }

  const unreadCount = computed(() => {
    return notificationList.value.filter((item) => !item.status).length;
  });

  const unreadCountText = computed(() => {
    return unreadCount.value ? `(${unreadCount.value})` : '';
  });

  async function fetchSourceData() {
    setLoading(true);
    try {
      const { data } = await queryNotificationList();
      const list = Array.isArray(data) ? data : data?.data || [];
      notificationList.value = list.map(mapNotification);
    } catch (err) {
      // error handled by interceptor
    } finally {
      setLoading(false);
    }
  }

  const handleItemClick = (items: MessageRecord[]) => {
    const promises = items.map((item) => markNotificationRead(String(item.id)));
    Promise.all(promises).then(() => fetchSourceData());
  };

  const handleAllRead = () => {
    markAllNotificationsRead().then(() => fetchSourceData());
  };

  const handleClear = () => {
    clearAllNotifications().then(() => fetchSourceData());
  };

  const handleViewMore = () => {
    router.push({ name: 'Notification' });
  };

  fetchSourceData();
</script>

<style scoped lang="less">
  :deep(.arco-popover-popup-content) {
    padding: 0;
  }

  :deep(.arco-list-item-meta) {
    align-items: flex-start;
  }

  .message-box {
    width: 400px;
  }

  .message-box-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 14px 16px 12px;
    border-bottom: 1px solid var(--color-neutral-3);
  }

  .header-title {
    color: var(--color-text-1);
    font-weight: 500;
    font-size: 14px;
  }

  :deep(.arco-result-subtitle) {
    color: rgb(var(--gray-6));
  }

  .message-box-footer {
    display: flex;
    height: 50px;
    line-height: 50px;
    border-top: 1px solid var(--color-neutral-3);

    .footer-item {
      flex: 1;
      text-align: center;
      border-right: 1px solid var(--color-neutral-3);

      &:last-child {
        border-right: none;
      }
    }
  }
</style>
