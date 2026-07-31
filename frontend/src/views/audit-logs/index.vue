<template>
  <div class="page-container device-platform-page">
    <section class="dp-overview">
      <div class="dp-overview-main">
        <div class="dp-kicker">{{ t('devicePlatform.kicker') }}</div>
        <h1 class="dp-title">{{ t('devicePlatform.audit.title') }}</h1>
        <p class="dp-description">{{ t('devicePlatform.audit.description') }}</p>
      </div>
      <div class="dp-overview-actions">
        <a-button @click="refresh">
          <template #icon><icon-refresh /></template>
          {{ t('devicePlatform.common.refresh') }}
        </a-button>
      </div>
      <div class="dp-metrics">
        <div class="dp-metric">
          <div class="dp-metric-label">{{ t('devicePlatform.audit.metric.total') }}</div>
          <div class="dp-metric-value">{{ pagination.total }}</div>
        </div>
        <div class="dp-metric is-green">
          <div class="dp-metric-label">{{ t('devicePlatform.audit.metric.projects') }}</div>
          <div class="dp-metric-value">{{ projectCount }}</div>
        </div>
        <div class="dp-metric is-purple">
          <div class="dp-metric-label">{{ t('devicePlatform.audit.metric.devices') }}</div>
          <div class="dp-metric-value">{{ deviceCount }}</div>
        </div>
        <div class="dp-metric is-orange">
          <div class="dp-metric-label">{{ t('devicePlatform.audit.metric.actors') }}</div>
          <div class="dp-metric-value">{{ actorCount }}</div>
        </div>
      </div>
    </section>

    <Grid :title="t('devicePlatform.audit.table.title')">
      <GridToolbar @refresh="refresh" />
      <GridTable
        class="dp-table"
        :loading="loading"
        :pagination="pagination"
        :data="audits"
        :columns="columns"
        :scroll="{ x: 1310 }"
        @page-change="onPageChange"
        @page-size-change="onPageSizeChange"
      >
        <template #actionName="{ record }">
          <div class="dp-cell-primary">{{ record.action }}</div>
          <div class="dp-cell-secondary dp-monospace">{{ record.id }}</div>
        </template>
        <template #project="{ record }">
          <a-typography-text v-if="record.project_id" class="dp-monospace" ellipsis>
            {{ record.project_id }}
          </a-typography-text>
          <span v-else class="dp-muted">{{ t('devicePlatform.common.notConfigured') }}</span>
        </template>
        <template #actor="{ record }">
          <div class="dp-cell-primary">{{ record.actor_type }}</div>
          <div class="dp-cell-secondary dp-monospace">{{ record.actor_id || '-' }}</div>
        </template>
        <template #resource="{ record }">
          <div class="dp-cell-primary">{{ record.resource_type }}</div>
          <div class="dp-cell-secondary dp-monospace">{{ record.resource_id || '-' }}</div>
        </template>
        <template #metadata="{ record }">
          <a-typography-text class="dp-monospace" ellipsis>
            {{ formatMetadata(record.metadata) }}
          </a-typography-text>
        </template>
      </GridTable>
    </Grid>
  </div>
</template>

<script lang="ts" setup>
  import { computed, onMounted, reactive, ref } from 'vue';
  import { useI18n } from 'vue-i18n';
  import useLoading from '@/hooks/loading';
  import { AuditLogRecord, queryAuditLogs } from '@/api/device-platform';
  import '../device-platform/workspace.less';

  defineOptions({ name: 'AuditLogsIndex' });

  const { t } = useI18n();
  const { loading, setLoading } = useLoading(false);
  const audits = ref<AuditLogRecord[]>([]);
  const pagination = reactive({
    current: 1,
    pageSize: 12,
    total: 0,
    showTotal: true,
    showPageSize: true,
  });
  const projectCount = computed(() => audits.value.filter((audit) => !!audit.project_id).length);
  const deviceCount = computed(() => audits.value.filter((audit) => audit.resource_type === 'device').length);
  const actorCount = computed(() => new Set(audits.value.map((audit) => `${audit.actor_type}:${audit.actor_id || ''}`)).size);
  const columns = computed(() => [
    { title: t('devicePlatform.audit.columns.action'), slotName: 'actionName', width: 260 },
    { title: t('devicePlatform.audit.columns.actor'), slotName: 'actor', width: 180 },
    { title: t('devicePlatform.common.project'), slotName: 'project', width: 220 },
    { title: t('devicePlatform.audit.columns.result'), dataIndex: 'result', width: 100 },
    { title: t('devicePlatform.audit.columns.resource'), slotName: 'resource', width: 190 },
    { title: t('devicePlatform.audit.columns.ip'), dataIndex: 'ip_address', width: 140 },
    { title: t('devicePlatform.audit.columns.metadata'), slotName: 'metadata', width: 320 },
    {
      title: t('devicePlatform.audit.columns.occurredAt'),
      dataIndex: 'occurred_at',
      ellipsis: true,
      tooltip: true,
      width: 190,
    },
  ]);

  const formatMetadata = (metadata: Record<string, unknown>) => {
    if (!metadata || Object.keys(metadata).length === 0) return t('devicePlatform.common.empty');
    return JSON.stringify(metadata);
  };

  const refresh = async () => {
    setLoading(true);
    try {
      const res = await queryAuditLogs({ page: pagination.current, page_size: pagination.pageSize });
      audits.value = res.data;
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

  onMounted(refresh);
</script>
