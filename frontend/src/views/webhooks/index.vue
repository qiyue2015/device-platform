<template>
  <div class="page-container device-platform-page">
    <section class="dp-overview">
      <div class="dp-overview-main">
        <div class="dp-kicker">{{ t('devicePlatform.kicker') }}</div>
        <h1 class="dp-title">{{ t('devicePlatform.webhooks.title') }}</h1>
        <p class="dp-description">{{ t('devicePlatform.webhooks.description') }}</p>
      </div>
      <div class="dp-overview-actions">
        <a-button @click="refresh">
          <template #icon><icon-refresh /></template>
          {{ t('devicePlatform.common.refresh') }}
        </a-button>
      </div>
      <div class="dp-metrics">
        <div class="dp-metric">
          <div class="dp-metric-label">{{ t('devicePlatform.webhooks.metric.total') }}</div>
          <div class="dp-metric-value">{{ webhooks.length }}</div>
        </div>
        <div class="dp-metric is-orange">
          <div class="dp-metric-label">{{ t('devicePlatform.webhooks.metric.pending') }}</div>
          <div class="dp-metric-value">{{ pendingCount }}</div>
        </div>
        <div class="dp-metric is-green">
          <div class="dp-metric-label">{{ t('devicePlatform.webhooks.metric.delivered') }}</div>
          <div class="dp-metric-value">{{ deliveredCount }}</div>
        </div>
        <div class="dp-metric is-red">
          <div class="dp-metric-label">{{ t('devicePlatform.webhooks.metric.failed') }}</div>
          <div class="dp-metric-value">{{ failedCount }}</div>
        </div>
      </div>
    </section>

    <Grid :title="t('devicePlatform.webhooks.table.title')">
      <GridToolbar @refresh="refresh" />
      <GridTable
        class="dp-table"
        :loading="loading"
        :pagination="{ pageSize: 10 }"
        :data="webhooks"
        :columns="columns"
        :scroll="{ x: 1220 }"
      >
        <template #event="{ record }">
          <div class="dp-cell-primary dp-monospace">{{ record.event_id }}</div>
          <div class="dp-cell-secondary dp-monospace">{{ record.id }}</div>
        </template>
        <template #target="{ record }">
          <a-typography-text class="dp-monospace" ellipsis copyable>
            {{ record.webhook_url }}
          </a-typography-text>
        </template>
        <template #webhookStatus="{ record }">
          <a-tag :color="getBusinessStatusMeta('webhook', record.status).color">
            {{ getBusinessStatusMeta('webhook', record.status).label }}
          </a-tag>
        </template>
        <template #attempts="{ record }">
          <span>{{ record.attempt_count }} / {{ record.max_attempts }}</span>
        </template>
        <template #webhookActions="{ record }">
          <a-button type="text" size="small" data-testid="resend-webhook" @click="handleResendWebhook(record.id)">
            {{ t('devicePlatform.webhooks.action.resend') }}
          </a-button>
        </template>
      </GridTable>
    </Grid>
  </div>
</template>

<script lang="ts" setup>
  import { computed, onMounted, ref } from 'vue';
  import { useI18n } from 'vue-i18n';
  import { Message } from '@arco-design/web-vue';
  import useLoading from '@/hooks/loading';
  import { getBusinessStatusMeta } from '@/utils/device-platform-status';
  import { WebhookDeliveryRecord, queryWebhookDeliveries, resendWebhookDelivery } from '@/api/device-platform';
  import '../device-platform/workspace.less';

  defineOptions({ name: 'WebhooksIndex' });

  const { t } = useI18n();
  const { loading, setLoading } = useLoading(false);
  const webhooks = ref<WebhookDeliveryRecord[]>([]);
  const pendingCount = computed(() => webhooks.value.filter((item) => ['pending', 'sending'].includes(item.status)).length);
  const deliveredCount = computed(() => webhooks.value.filter((item) => item.status === 'delivered').length);
  const failedCount = computed(() => webhooks.value.filter((item) => ['failed', 'dead'].includes(item.status)).length);
  const columns = computed(() => [
    { title: t('devicePlatform.webhooks.columns.event'), slotName: 'event', width: 260 },
    { title: t('devicePlatform.webhooks.columns.url'), slotName: 'target', width: 360 },
    { title: t('devicePlatform.common.status'), slotName: 'webhookStatus', width: 130 },
    { title: t('devicePlatform.webhooks.columns.attempts'), slotName: 'attempts', width: 110 },
    {
      title: t('devicePlatform.webhooks.columns.lastError'),
      dataIndex: 'last_error',
      ellipsis: true,
      tooltip: true,
      width: 260,
    },
    { title: t('devicePlatform.common.actions'), slotName: 'webhookActions', width: 100 },
  ]);

  const refresh = async () => {
    setLoading(true);
    try {
      const res = await queryWebhookDeliveries();
      webhooks.value = res.data;
    } finally {
      setLoading(false);
    }
  };

  const handleResendWebhook = async (id: string) => {
    setLoading(true);
    try {
      await resendWebhookDelivery(id);
      await refresh();
      Message.success(t('devicePlatform.webhooks.message.queued'));
    } finally {
      setLoading(false);
    }
  };

  onMounted(refresh);
</script>
