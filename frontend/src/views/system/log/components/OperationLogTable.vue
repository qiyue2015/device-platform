<template>
  <Grid>
    <GridToolbar @refresh="fetchData">
      <template #prepend>
        <a-input-search
          v-model="searchKeyword"
          :placeholder="$t('system.log.search.placeholder')"
          style="width: 280px"
          @search="handleSearch"
          @press-enter="handleSearch"
        />
      </template>
    </GridToolbar>
    <GridTable
      :loading="loading"
      :data="tableData"
      :columns="columns"
      :pagination="pagination"
      @page-change="onPageChange"
      @page-size-change="onPageSizeChange"
    >
      <template #createdAt="{ record }">
        <a-link @click="openDetail(record)">{{ record.created_at ?? '-' }}</a-link>
      </template>
      <template #logName="{ record }">
        <a-tag size="small">{{ $t(`system.log.module.${record.log_name}`) }}</a-tag>
      </template>
      <template #event="{ record }">
        <a-tag v-if="record.event" size="small" :color="eventColor(record.event)">
          {{ $t(`system.log.event.${record.event}`) }}
        </a-tag>
        <span v-else>-</span>
      </template>
    </GridTable>
    <!-- Detail Drawer -->
    <a-drawer
      :visible="drawerVisible"
      :title="$t('system.log.operation.detail')"
      :width="560"
      unmount-on-close
      @cancel="drawerVisible = false"
    >
      <a-descriptions :column="1" bordered size="medium">
        <a-descriptions-item :label="$t('system.log.op.createdAt')">
          {{ currentRecord?.created_at ?? '-' }}
        </a-descriptions-item>
        <a-descriptions-item :label="$t('system.log.op.causer')">
          {{ currentRecord?.causer?.name ?? '-' }}
          <span v-if="currentRecord?.causer?.email" style="margin-left: 4px; color: var(--color-text-3)">
            ({{ currentRecord.causer.email }})
          </span>
        </a-descriptions-item>
        <a-descriptions-item :label="$t('system.log.op.subject')">
          {{ subjectLabel(currentRecord) }}
        </a-descriptions-item>
        <a-descriptions-item :label="$t('system.log.op.subject.type')">
          {{ subjectTypeLabel(currentRecord?.subject_type ?? null) }}
        </a-descriptions-item>
        <a-descriptions-item :label="$t('system.log.op.description')">
          {{ currentRecord?.description ?? '-' }}
        </a-descriptions-item>
        <a-descriptions-item :label="$t('system.log.op.module')">
          {{ currentRecord?.log_name ? $t(`system.log.module.${currentRecord.log_name}`) : '-' }}
        </a-descriptions-item>
        <a-descriptions-item :label="$t('system.log.op.event')">
          <a-tag v-if="currentRecord?.event" size="small" :color="eventColor(currentRecord.event)">
            {{ $t(`system.log.event.${currentRecord.event}`) }}
          </a-tag>
          <span v-else>-</span>
        </a-descriptions-item>
      </a-descriptions>
      <a-divider>{{ $t('system.log.operation.properties') }}</a-divider>
      <pre class="log-properties">{{ formatProperties(currentRecord?.properties) }}</pre>
    </a-drawer>
  </Grid>
</template>

<script lang="ts" setup>
  import { ref, reactive, computed, onMounted } from 'vue';
  import { useI18n } from 'vue-i18n';
  import { useLoading } from '@/hooks';
  import { HttpResponse } from '@/api/interceptor';
  import { queryLogList, type LogRecord } from '@/api/system/log';
  import { UAParser } from 'ua-parser-js';

  const { t } = useI18n();
  const { loading, setLoading } = useLoading(false);
  const searchKeyword = ref('');
  const tableData = ref<LogRecord[]>([]);
  const drawerVisible = ref(false);
  const currentRecord = ref<LogRecord | null>(null);

  const prop = (record: LogRecord, key: string) => (record.properties as Record<string, unknown>)?.[key] ?? '-';

  const parseUA = (record: LogRecord) => {
    const ua = (record.properties as Record<string, unknown>)?.user_agent;
    if (!ua) return { browser: '-', os: '-' };
    const result = new UAParser(String(ua)).getResult();
    const browser = result.browser.name ? `${result.browser.name} ${result.browser.major ?? ''}`.trim() : '-';
    const os = result.os.name ? `${result.os.name} ${result.os.version ?? ''}`.trim() : '-';
    return { browser, os };
  };

  const eventColor = (event: string) => {
    const map: Record<string, string> = {
      created: 'green',
      updated: 'blue',
      deleted: 'red',
      status_toggled: 'orangered',
      password_reset: 'purple',
      roles_assigned: 'cyan',
    };
    return map[event] ?? 'gray';
  };

  const subjectLabel = (record: LogRecord | null) => {
    if (!record) return '-';
    const name = record.subject?.name;
    const id = record.subject_id;
    if (name) return `${name} (ID: ${id})`;
    if (id) return `ID: ${id}`;
    return '-';
  };

  const subjectTypeLabel = (subjectType: string | null) => {
    if (!subjectType) return '-';
    const parts = subjectType.split('\\');
    return parts[parts.length - 1] || subjectType;
  };

  const formatProperties = (properties: Record<string, unknown> | null | undefined) => {
    if (!properties || (Array.isArray(properties) && properties.length === 0)) return '-';
    return JSON.stringify(properties, null, 2);
  };

  const pagination = reactive({
    current: 1,
    pageSize: 20,
    total: 0,
    showTotal: true,
    showPageSize: true,
  });

  const columns = computed(() => [
    { title: t('system.log.op.createdAt'), slotName: 'createdAt', width: 190 },
    {
      title: t('system.log.op.causer'),
      render: ({ record }: { record: LogRecord }) => record.causer?.name ?? '-',
      width: 140,
    },
    { title: t('system.log.op.description'), dataIndex: 'description', width: 200, ellipsis: true, tooltip: true },
    { title: t('system.log.op.module'), slotName: 'logName', width: 300 },
    {
      title: t('system.log.auth.ip'),
      render: ({ record }: { record: LogRecord }) => prop(record, 'ip'),
      width: 140,
    },
    {
      title: t('system.log.op.location'),
      render: ({ record }: { record: LogRecord }) => prop(record, 'location'),
      width: 140,
      ellipsis: true,
      tooltip: true,
    },
    {
      title: t('system.log.auth.browser'),
      render: ({ record }: { record: LogRecord }) => parseUA(record).browser,
      width: 120,
    },
    {
      title: t('system.log.auth.os'),
      render: ({ record }: { record: LogRecord }) => parseUA(record).os,
      width: 140,
    },
  ]);

  const openDetail = (record: LogRecord) => {
    currentRecord.value = record;
    drawerVisible.value = true;
  };

  const fetchData = async () => {
    setLoading(true);
    try {
      const res = (await queryLogList({
        log_name: ['user', 'role', 'menu', 'dict_type', 'dict_item'],
        description: searchKeyword.value || undefined,
        sort: '-created_at',
        current: pagination.current,
        pageSize: pagination.pageSize,
      })) as unknown as HttpResponse<LogRecord[]>;
      tableData.value = res.data;
      pagination.total = res.meta?.total ?? 0;
    } finally {
      setLoading(false);
    }
  };

  const handleSearch = () => {
    pagination.current = 1;
    fetchData();
  };
  const onPageChange = (p: number) => {
    pagination.current = p;
    fetchData();
  };
  const onPageSizeChange = (s: number) => {
    pagination.pageSize = s;
    pagination.current = 1;
    fetchData();
  };

  onMounted(() => {
    fetchData();
  });
</script>

<style scoped>
  .log-properties {
    padding: 12px;
    font-size: 13px;
    line-height: 1.6;
    white-space: pre-wrap;
    word-break: break-all;
    background-color: var(--color-fill-2);
    border-radius: 4px;
  }
</style>
