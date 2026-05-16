<template>
  <div class="page-container">
    <a-card :title="$t('menu.auditLogs')" :bordered="false">
      <a-table row-key="id" :loading="loading" :pagination="{ pageSize: 12 }" :data="audits" :columns="columns" />
    </a-card>
  </div>
</template>

<script lang="ts" setup>
  import { computed, onMounted, ref } from 'vue';
  import useLoading from '@/hooks/loading';
  import { AuditLogRecord, queryAuditLogs } from '@/api/device-platform';

  defineOptions({ name: 'AuditLogsIndex' });

  const { loading, setLoading } = useLoading(false);
  const audits = ref<AuditLogRecord[]>([]);
  const columns = computed(() => [
    { title: 'Action', dataIndex: 'action' },
    { title: 'Actor', dataIndex: 'actor_id' },
    { title: 'Project', dataIndex: 'project_id', ellipsis: true, tooltip: true },
    { title: 'IP', dataIndex: 'ip' },
    { title: 'Created', dataIndex: 'created_at', ellipsis: true, tooltip: true },
  ]);

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
