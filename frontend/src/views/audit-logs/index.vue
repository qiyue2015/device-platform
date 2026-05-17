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
          <div class="dp-metric-value">{{ audits.length }}</div>
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
        :pagination="{ pageSize: 12 }"
        :data="audits"
        :columns="columns"
        :scroll="{ x: 1310 }"
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
        <template #summary="{ record }">
          <a-typography-text class="dp-monospace" ellipsis>
            {{ formatSummary(record.summary) }}
          </a-typography-text>
        </template>
      </GridTable>
    </Grid>
  </div>
</template>

<script lang="ts" setup>
  import { computed, onMounted, ref } from 'vue';
  import { useI18n } from 'vue-i18n';
  import useLoading from '@/hooks/loading';
  import { AuditLogRecord, queryAuditLogs } from '@/api/device-platform';
  import '../device-platform/workspace.less';

  defineOptions({ name: 'AuditLogsIndex' });

  const { t } = useI18n();
  const { loading, setLoading } = useLoading(false);
  const audits = ref<AuditLogRecord[]>([]);
  const projectCount = computed(() => audits.value.filter((audit) => !!audit.project_id).length);
  const deviceCount = computed(() => audits.value.filter((audit) => !!audit.device_id).length);
  const actorCount = computed(() => new Set(audits.value.map((audit) => audit.actor_id).filter(Boolean)).size);
  const columns = computed(() => [
    { title: t('devicePlatform.audit.columns.action'), slotName: 'actionName', width: 260 },
    { title: t('devicePlatform.audit.columns.actor'), dataIndex: 'actor_id', ellipsis: true, tooltip: true, width: 180 },
    { title: t('devicePlatform.common.project'), slotName: 'project', width: 220 },
    { title: t('devicePlatform.audit.columns.ip'), dataIndex: 'ip', width: 140 },
    { title: t('devicePlatform.audit.columns.summary'), slotName: 'summary', width: 320 },
    { title: t('devicePlatform.common.createdAt'), dataIndex: 'created_at', ellipsis: true, tooltip: true, width: 190 },
  ]);

  const formatSummary = (summary: Record<string, unknown>) => {
    if (!summary || Object.keys(summary).length === 0) return t('devicePlatform.common.empty');
    return JSON.stringify(summary);
  };

  const refresh = async () => {
    setLoading(true);
    try {
      const res = await queryAuditLogs();
      audits.value = res.data;
    } finally {
      setLoading(false);
    }
  };

  onMounted(refresh);
</script>
