<template>
  <div class="page-container device-platform-page">
    <section class="dp-overview">
      <div class="dp-overview-main">
        <div class="dp-kicker">{{ t('devicePlatform.kicker') }}</div>
        <h1 class="dp-title">{{ t('devicePlatform.events.title') }}</h1>
        <p class="dp-description">{{ t('devicePlatform.events.description') }}</p>
      </div>
      <div class="dp-overview-actions">
        <a-button :loading="loading" @click="refresh">
          <template #icon><icon-refresh /></template>
          {{ t('devicePlatform.common.refresh') }}
        </a-button>
      </div>
      <div class="dp-metrics">
        <div class="dp-metric">
          <div class="dp-metric-label">{{ t('devicePlatform.events.metric.total') }}</div>
          <div class="dp-metric-value">{{ pagination.total }}</div>
        </div>
        <div class="dp-metric is-green">
          <div class="dp-metric-label">{{ t('devicePlatform.events.metric.devicePage') }}</div>
          <div class="dp-metric-value">{{ deviceEventCount }}</div>
        </div>
        <div class="dp-metric is-purple">
          <div class="dp-metric-label">{{ t('devicePlatform.events.metric.commandPage') }}</div>
          <div class="dp-metric-value">{{ commandEventCount }}</div>
        </div>
        <div class="dp-metric is-orange">
          <div class="dp-metric-label">{{ t('devicePlatform.events.metric.sourcesPage') }}</div>
          <div class="dp-metric-value">{{ sourceCount }}</div>
        </div>
      </div>
    </section>

    <a-alert type="info">{{ t('devicePlatform.events.evidenceNotice') }}</a-alert>

    <Grid :title="t('devicePlatform.events.table.title')">
      <GridToolbar @refresh="refresh">
        <template #prepend>
          <a-form :model="filters" layout="inline" class="dp-inline-form events-filter-form">
            <a-form-item :label="t('devicePlatform.events.filter.project')">
              <a-input
                v-model="filters.project_id"
                class="dp-toolbar-control"
                allow-clear
                :placeholder="t('devicePlatform.events.filter.project.placeholder')"
                @press-enter="applyFilters"
              />
            </a-form-item>
            <a-form-item :label="t('devicePlatform.events.filter.device')">
              <a-input
                v-model="filters.device_id"
                class="dp-toolbar-control"
                allow-clear
                :placeholder="t('devicePlatform.events.filter.device.placeholder')"
                @press-enter="applyFilters"
              />
            </a-form-item>
            <a-form-item :label="t('devicePlatform.events.filter.command')">
              <a-input
                v-model="filters.command_id"
                class="dp-toolbar-control"
                allow-clear
                :placeholder="t('devicePlatform.events.filter.command.placeholder')"
                @press-enter="applyFilters"
              />
            </a-form-item>
            <a-form-item :label="t('devicePlatform.events.filter.eventType')">
              <a-select
                v-model="filters.event_type"
                class="dp-toolbar-control is-wide"
                allow-clear
                :placeholder="t('devicePlatform.common.all')"
              >
                <a-option v-for="eventType in eventTypes" :key="eventType" :value="eventType">
                  {{ eventType }}
                </a-option>
              </a-select>
            </a-form-item>
            <a-form-item class="dp-inline-action">
              <a-space>
                <a-button type="primary" :loading="loading" @click="applyFilters">
                  <template #icon><icon-search /></template>
                  {{ t('devicePlatform.events.action.search') }}
                </a-button>
                <a-button @click="resetFilters">{{ t('devicePlatform.events.action.reset') }}</a-button>
              </a-space>
            </a-form-item>
          </a-form>
        </template>
      </GridToolbar>
      <GridTable
        class="dp-table"
        :loading="loading"
        :data="events"
        :columns="columns"
        row-key="event_id"
        :pagination="pagination"
        :scroll="{ x: 1260 }"
        @page-change="onPageChange"
        @page-size-change="onPageSizeChange"
      >
        <template #event="{ record }">
          <div class="dp-cell-primary">{{ record.event_type }}</div>
          <div class="dp-cell-secondary dp-monospace">{{ record.event_id }}</div>
        </template>
        <template #project="{ record }">
          <a-typography-text class="dp-monospace" ellipsis>{{ record.project_id }}</a-typography-text>
        </template>
        <template #relations="{ record }">
          <div class="dp-cell-primary dp-monospace">
            {{ t('devicePlatform.events.columns.deviceShort') }}: {{ record.device_id || '-' }}
          </div>
          <div class="dp-cell-secondary dp-monospace">
            {{ t('devicePlatform.events.columns.commandShort') }}: {{ record.command_id || '-' }}
          </div>
        </template>
        <template #source="{ record }">
          <a-tag color="arcoblue">{{ record.source }}</a-tag>
        </template>
        <template #action="{ record }">
          <a-button type="text" size="small" @click="loadEventDetail(record)">
            {{ t('devicePlatform.events.action.detail') }}
          </a-button>
        </template>
      </GridTable>
    </Grid>

    <a-drawer
      v-model:visible="detailVisible"
      :title="t('devicePlatform.events.drawer.title')"
      :footer="false"
      width="min(100vw, 600px)"
    >
      <a-skeleton v-if="detailLoading" animation />
      <template v-else-if="eventDetail">
        <a-descriptions :column="1" size="small" bordered>
          <a-descriptions-item :label="t('devicePlatform.events.detail.eventId')">
            <a-typography-text class="dp-monospace" copyable>{{ eventDetail.event_id }}</a-typography-text>
          </a-descriptions-item>
          <a-descriptions-item :label="t('devicePlatform.events.detail.eventType')">
            <span class="dp-monospace">{{ eventDetail.event_type }}</span>
          </a-descriptions-item>
          <a-descriptions-item :label="t('devicePlatform.events.detail.source')">
            {{ eventDetail.source }}
          </a-descriptions-item>
          <a-descriptions-item :label="t('devicePlatform.events.detail.occurredAt')">
            {{ eventDetail.occurred_at }}
          </a-descriptions-item>
          <a-descriptions-item :label="t('devicePlatform.common.project')">
            <span class="dp-monospace">{{ eventDetail.project_id }}</span>
          </a-descriptions-item>
          <a-descriptions-item :label="t('devicePlatform.common.device')">
            <span class="dp-monospace">{{ eventDetail.device_id || '-' }}</span>
          </a-descriptions-item>
          <a-descriptions-item :label="t('devicePlatform.events.detail.command')">
            <span class="dp-monospace">{{ eventDetail.command_id || '-' }}</span>
          </a-descriptions-item>
          <a-descriptions-item :label="t('devicePlatform.events.detail.schemaVersion')">
            {{ eventDetail.schema_version }}
          </a-descriptions-item>
        </a-descriptions>
        <h3 class="dp-panel-title events-payload-title">{{ t('devicePlatform.events.detail.payload') }}</h3>
        <pre class="dp-json">{{ JSON.stringify(eventDetail.payload, null, 2) }}</pre>
      </template>
    </a-drawer>
  </div>
</template>

<script lang="ts" setup>
  import { computed, onMounted, reactive, ref } from 'vue';
  import { useI18n } from 'vue-i18n';
  import useLoading from '@/hooks/loading';
  import { EventRecord, queryEventDetail, queryEvents } from '@/api/device-platform';
  import '../device-platform/workspace.less';

  defineOptions({ name: 'EventsIndex' });

  const eventTypes = [
    'device.created',
    'device.lifecycle_changed',
    'device.connection_changed',
    'device.state_updated',
    'command.created',
    'command.status_changed',
    'command.evidence_updated',
  ];

  const { t } = useI18n();
  const { loading, setLoading } = useLoading(false);
  const events = ref<EventRecord[]>([]);
  const eventDetail = ref<EventRecord>();
  const detailVisible = ref(false);
  const detailLoading = ref(false);
  const filters = reactive({
    project_id: '',
    device_id: '',
    command_id: '',
    event_type: '',
  });
  const pagination = reactive({
    current: 1,
    pageSize: 10,
    total: 0,
    showTotal: true,
    showPageSize: true,
  });

  const deviceEventCount = computed(() => events.value.filter((event) => !!event.device_id).length);
  const commandEventCount = computed(() => events.value.filter((event) => !!event.command_id).length);
  const sourceCount = computed(() => new Set(events.value.map((event) => event.source)).size);
  const columns = computed(() => [
    { title: t('devicePlatform.events.columns.event'), slotName: 'event', width: 320 },
    { title: t('devicePlatform.common.project'), slotName: 'project', width: 240 },
    { title: t('devicePlatform.events.columns.relations'), slotName: 'relations', width: 300 },
    { title: t('devicePlatform.events.columns.source'), slotName: 'source', width: 140 },
    {
      title: t('devicePlatform.events.columns.occurredAt'),
      dataIndex: 'occurred_at',
      ellipsis: true,
      tooltip: true,
      width: 190,
    },
    { title: t('devicePlatform.common.actions'), slotName: 'action', width: 100 },
  ]);

  const refresh = async () => {
    setLoading(true);
    try {
      const res = await queryEvents({
        project_id: filters.project_id.trim() || undefined,
        device_id: filters.device_id.trim() || undefined,
        command_id: filters.command_id.trim() || undefined,
        event_type: filters.event_type || undefined,
        page: pagination.current,
        page_size: pagination.pageSize,
      });
      events.value = res.data;
      pagination.total = res.meta?.total ?? res.data.length;
    } finally {
      setLoading(false);
    }
  };

  const applyFilters = () => {
    pagination.current = 1;
    refresh();
  };

  const resetFilters = () => {
    filters.project_id = '';
    filters.device_id = '';
    filters.command_id = '';
    filters.event_type = '';
    applyFilters();
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

  const loadEventDetail = async (record: EventRecord) => {
    detailVisible.value = true;
    detailLoading.value = true;
    eventDetail.value = undefined;
    try {
      const res = await queryEventDetail(record.event_id);
      eventDetail.value = res.data;
    } finally {
      detailLoading.value = false;
    }
  };

  onMounted(refresh);
</script>

<style lang="less" scoped>
  .events-payload-title {
    margin-top: 20px;
  }

  @media (width <= 768px) {
    .events-filter-form :deep(.arco-space),
    .events-filter-form :deep(.arco-space-item),
    .events-filter-form :deep(.arco-btn) {
      width: 100%;
    }
  }
</style>
