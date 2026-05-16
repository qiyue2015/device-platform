<template>
  <div class="page-container">
    <a-card :title="$t('menu.webhooks')" :bordered="false">
      <a-table row-key="id" :loading="loading" :pagination="{ pageSize: 10 }" :data="webhooks" :columns="columns">
        <template #webhookStatus="{ record }">
          <a-tag :color="statusColor(record.status)">{{ record.status }}</a-tag>
        </template>
        <template #webhookActions="{ record }">
          <a-button size="mini" data-testid="resend-webhook" @click="handleResendWebhook(record.id)">Resend</a-button>
        </template>
      </a-table>
    </a-card>
  </div>
</template>

<script lang="ts" setup>
  import { computed, onMounted, ref } from 'vue';
  import { Message } from '@arco-design/web-vue';
  import useLoading from '@/hooks/loading';
  import { WebhookDeliveryRecord, queryWebhookDeliveries, resendWebhookDelivery } from '@/api/device-platform';

  defineOptions({ name: 'WebhooksIndex' });

  const { loading, setLoading } = useLoading(false);
  const webhooks = ref<WebhookDeliveryRecord[]>([]);
  const columns = computed(() => [
    { title: 'ID', dataIndex: 'id', ellipsis: true, tooltip: true },
    { title: 'Event', dataIndex: 'event_id', ellipsis: true, tooltip: true },
    { title: 'Status', slotName: 'webhookStatus' },
    { title: 'Attempts', dataIndex: 'attempt_count', width: 90 },
    { title: 'Last Error', dataIndex: 'last_error', ellipsis: true, tooltip: true },
    { title: 'Actions', slotName: 'webhookActions', width: 100 },
  ]);

  const statusColor = (status: string) => {
    if (['delivered'].includes(status)) return 'green';
    if (['failed', 'dead'].includes(status)) return 'red';
    if (['pending'].includes(status)) return 'orange';
    return 'arcoblue';
  };

  const refresh = async () => {
    const res = await queryWebhookDeliveries();
    webhooks.value = res.data;
  };

  const handleResendWebhook = async (id: string) => {
    setLoading(true);
    try {
      await resendWebhookDelivery(id);
      await refresh();
      Message.success('Webhook resend queued');
    } finally {
      setLoading(false);
    }
  };

  onMounted(refresh);
</script>
