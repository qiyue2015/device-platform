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
      <template #event="{ record }">
        <a-tag v-if="record.event" size="small" :color="record.event === 'login_success' ? 'green' : 'red'">
          {{ $t(`system.log.event.${record.event}`) }}
        </a-tag>
        <span v-else>-</span>
      </template>
      <template #status="{ record }">
        <a-tag size="small" :color="record.event === 'login_success' ? 'green' : 'red'">
          {{ record.event === 'login_success' ? $t('system.log.status.success') : $t('system.log.status.fail') }}
        </a-tag>
      </template>
    </GridTable>
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

  const prop = (record: LogRecord, key: string) => (record.properties as Record<string, unknown>)?.[key] ?? '-';

  const parseUA = (record: LogRecord) => {
    const ua = (record.properties as Record<string, unknown>)?.user_agent;
    if (!ua) return { browser: '-', os: '-' };
    const result = new UAParser(String(ua)).getResult();
    const browser = result.browser.name ? `${result.browser.name} ${result.browser.major ?? ''}`.trim() : '-';
    const os = result.os.name ? `${result.os.name} ${result.os.version ?? ''}`.trim() : '-';
    return { browser, os };
  };

  const getCauserName = (record: LogRecord) => {
    if (record.causer?.name) return record.causer.name;
    const email = (record.properties as Record<string, unknown>)?.email;
    return email ? String(email) : '-';
  };

  const pagination = reactive({
    current: 1,
    pageSize: 20,
    total: 0,
    showTotal: true,
    showPageSize: true,
  });

  const columns = computed(() => [
    { title: t('system.log.auth.createdAt'), dataIndex: 'created_at', width: 190 },
    {
      title: t('system.log.auth.causer'),
      render: ({ record }: { record: LogRecord }) => getCauserName(record),
      width: 180,
      ellipsis: true,
      tooltip: true,
    },
    { title: t('system.log.auth.event'), slotName: 'event', width: 120 },
    { title: t('system.log.auth.status'), slotName: 'status', width: 80 },
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

  const fetchData = async () => {
    setLoading(true);
    try {
      const res = (await queryLogList({
        log_name: ['auth'],
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
