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
          <div class="dp-metric-value">{{ pagination.total }}</div>
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
        :pagination="pagination"
        :data="webhooks"
        :columns="columns"
        :scroll="{ x: 1220 }"
        @page-change="onPageChange"
        @page-size-change="onPageSizeChange"
      >
        <template #event="{ record }">
          <div class="dp-cell-primary dp-monospace">{{ record.event_id }}</div>
          <div class="dp-cell-secondary dp-monospace">{{ record.id }}</div>
        </template>
        <template #target="{ record }">
          <a-typography-text class="dp-monospace" ellipsis copyable>
            {{ record.target_url }}
          </a-typography-text>
        </template>
        <template #webhookStatus="{ record }">
          <a-tag :color="getBusinessStatusMeta('webhook', record.status).color">
            {{ getBusinessStatusMeta('webhook', record.status).label }}
          </a-tag>
        </template>
        <template #attempts="{ record }">
          <span>{{ record.attempt_count }}</span>
        </template>
        <template #webhookActions="{ record }">
          <a-space>
            <a-button type="text" size="small" @click="loadDeliveryDetail(record.id)">
              {{ t('devicePlatform.webhooks.action.detail') }}
            </a-button>
            <a-popconfirm
              :content="t('devicePlatform.webhooks.confirm.resend')"
              :disabled="record.status !== 'dead'"
              @ok="handleResendWebhook(record.id)"
            >
              <a-button type="text" size="small" data-testid="resend-webhook" :disabled="record.status !== 'dead'">
                {{ t('devicePlatform.webhooks.action.resend') }}
              </a-button>
            </a-popconfirm>
          </a-space>
        </template>
      </GridTable>
    </Grid>

    <a-drawer
      v-model:visible="detailVisible"
      :title="t('devicePlatform.webhooks.drawer.title')"
      :footer="false"
      width="min(100vw, 600px)"
    >
      <a-skeleton v-if="detailLoading" animation />
      <template v-else-if="deliveryDetail">
        <a-descriptions :column="1" size="small" bordered>
          <a-descriptions-item :label="t('devicePlatform.common.status')">{{ deliveryDetail.status }}</a-descriptions-item>
          <a-descriptions-item :label="t('devicePlatform.webhooks.columns.url')">
            <span class="dp-monospace">{{ deliveryDetail.target_url }}</span>
          </a-descriptions-item>
          <a-descriptions-item :label="t('devicePlatform.webhooks.columns.configVersion')">
            {{ deliveryDetail.webhook_config_version }}
          </a-descriptions-item>
          <a-descriptions-item :label="t('devicePlatform.webhooks.columns.replayOf')">
            <span class="dp-monospace">{{ deliveryDetail.replay_of_delivery_id || '-' }}</span>
          </a-descriptions-item>
        </a-descriptions>
        <h3 class="dp-panel-title">{{ t('devicePlatform.webhooks.detail.attempts') }}</h3>
        <pre class="dp-json">{{ JSON.stringify(deliveryDetail.attempts || [], null, 2) }}</pre>
      </template>
    </a-drawer>
  </div>
</template>

<script lang="ts" setup>
  import { computed, onMounted, reactive, ref } from 'vue';
  import { useI18n } from 'vue-i18n';
  import { Message } from '@arco-design/web-vue';
  import useLoading from '@/hooks/loading';
  import { getBusinessStatusMeta } from '@/utils/device-platform-status';
  import {
    WebhookDeliveryRecord,
    queryWebhookDeliveries,
    queryWebhookDeliveryDetail,
    resendWebhookDelivery,
  } from '@/api/device-platform';
  import '../device-platform/workspace.less';

  defineOptions({ name: 'WebhooksIndex' });

  const { t } = useI18n();
  const { loading, setLoading } = useLoading(false);
  const webhooks = ref<WebhookDeliveryRecord[]>([]);
  const deliveryDetail = ref<WebhookDeliveryRecord>();
  const detailVisible = ref(false);
  const detailLoading = ref(false);
  const pagination = reactive({
    current: 1,
    pageSize: 10,
    total: 0,
    showTotal: true,
    showPageSize: true,
  });
  const pendingCount = computed(() => webhooks.value.filter((item) => ['pending', 'sending'].includes(item.status)).length);
  const deliveredCount = computed(() => webhooks.value.filter((item) => item.status === 'delivered').length);
  const failedCount = computed(() => webhooks.value.filter((item) => ['failed', 'dead'].includes(item.status)).length);
  const columns = computed(() => [
    { title: t('devicePlatform.webhooks.columns.event'), slotName: 'event', width: 260 },
    { title: t('devicePlatform.webhooks.columns.url'), slotName: 'target', width: 360 },
    { title: t('devicePlatform.common.status'), slotName: 'webhookStatus', width: 130 },
    { title: t('devicePlatform.webhooks.columns.attempts'), slotName: 'attempts', width: 110 },
    { title: t('devicePlatform.webhooks.columns.nextAttempt'), dataIndex: 'next_attempt_at', width: 190 },
    { title: t('devicePlatform.common.createdAt'), dataIndex: 'created_at', width: 190 },
    { title: t('devicePlatform.common.actions'), slotName: 'webhookActions', width: 170 },
  ]);

  const refresh = async () => {
    setLoading(true);
    try {
      const res = await queryWebhookDeliveries({ page: pagination.current, page_size: pagination.pageSize });
      webhooks.value = res.data;
      pagination.total = res.meta?.total ?? res.data.length;
    } finally {
      setLoading(false);
    }
  };

  const onPageChange = (page: number) => {
    pagination.current = page;
    refresh();
  };

  const onPageSizeChange = (pageSize: number) => {
    pagination.pageSize = pageSize;
    pagination.current = 1;
    refresh();
  };

  const loadDeliveryDetail = async (id: string) => {
    detailVisible.value = true;
    detailLoading.value = true;
    deliveryDetail.value = undefined;
    try {
      const res = await queryWebhookDeliveryDetail(id);
      deliveryDetail.value = res.data;
    } finally {
      detailLoading.value = false;
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
