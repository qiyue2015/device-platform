<template>
  <div class="page-container device-platform-page">
    <section class="dp-overview">
      <div class="dp-overview-main">
        <div class="dp-kicker">{{ t('devicePlatform.kicker') }}</div>
        <h1 class="dp-title">{{ t('devicePlatform.commands.title') }}</h1>
        <p class="dp-description">{{ t('devicePlatform.commands.description') }}</p>
      </div>
      <div class="dp-overview-actions">
        <a-button @click="refresh">
          <template #icon><icon-refresh /></template>
          {{ t('devicePlatform.common.refresh') }}
        </a-button>
      </div>
      <div class="dp-metrics">
        <div class="dp-metric">
          <div class="dp-metric-label">{{ t('devicePlatform.commands.metric.total') }}</div>
          <div class="dp-metric-value">{{ commands.length }}</div>
        </div>
        <div class="dp-metric is-orange">
          <div class="dp-metric-label">{{ t('devicePlatform.commands.metric.active') }}</div>
          <div class="dp-metric-value">{{ activeCount }}</div>
        </div>
        <div class="dp-metric is-green">
          <div class="dp-metric-label">{{ t('devicePlatform.commands.metric.success') }}</div>
          <div class="dp-metric-value">{{ successCount }}</div>
        </div>
        <div class="dp-metric is-red">
          <div class="dp-metric-label">{{ t('devicePlatform.commands.metric.failed') }}</div>
          <div class="dp-metric-value">{{ failedCount }}</div>
        </div>
      </div>
    </section>

    <Grid :title="t('devicePlatform.commands.table.title')">
      <GridToolbar @refresh="refreshProjectData">
        <template #prepend>
          <a-space wrap>
            <a-select
              v-model="selectedProjectId"
              class="dp-toolbar-control"
              :placeholder="t('devicePlatform.commands.filter.project')"
            >
              <a-option v-for="project in projects" :key="project.id" :value="project.id">{{ project.name }}</a-option>
            </a-select>
            <a-select
              v-model="selectedDeviceId"
              class="dp-toolbar-control"
              :placeholder="t('devicePlatform.commands.filter.device')"
            >
              <a-option v-for="device in devices" :key="device.id" :value="device.id">
                {{ device.name }} · {{ device.provider_device_id }}
              </a-option>
            </a-select>
            <a-select v-model="commandForm.command_type" class="dp-toolbar-control" data-testid="command-type">
              <a-option value="unlock">unlock</a-option>
              <a-option value="lock">lock</a-option>
              <a-option value="query_status">query_status</a-option>
              <a-option value="set_config">set_config</a-option>
              <a-option value="reboot">reboot</a-option>
            </a-select>
            <a-input
              v-model="commandForm.idempotency_key"
              class="dp-toolbar-control is-wide"
              :placeholder="t('devicePlatform.commands.form.idempotency.placeholder')"
            />
            <a-button
              type="primary"
              data-testid="send-command"
              :disabled="!selectedDeviceId"
              :loading="loading"
              @click="handleCreateCommand"
            >
              <template #icon><icon-send /></template>
              {{ t('devicePlatform.commands.action.send') }}
            </a-button>
          </a-space>
        </template>
      </GridToolbar>
      <GridTable
        class="dp-table"
        :loading="loading"
        :data="commands"
        :columns="columns"
        :pagination="{ pageSize: 10 }"
        :scroll="{ x: 1100 }"
      >
        <template #command="{ record }">
          <div class="dp-cell-primary">{{ record.command_type }}</div>
          <div class="dp-cell-secondary dp-monospace">{{ record.id }}</div>
        </template>
        <template #device="{ record }">
          <a-typography-text class="dp-monospace" ellipsis>{{ record.device_id }}</a-typography-text>
        </template>
        <template #commandStatus="{ record }">
          <a-tag :color="getBusinessStatusMeta('command', record.status).color">
            {{ getBusinessStatusMeta('command', record.status).label }}
          </a-tag>
        </template>
        <template #action="{ record }">
          <a-button type="text" size="small" @click="loadCommandDetail(record)">
            {{ t('devicePlatform.commands.action.detail') }}
          </a-button>
        </template>
      </GridTable>
    </Grid>

    <a-drawer v-model:visible="detailVisible" :title="t('devicePlatform.commands.drawer.title')" width="520px">
      <a-skeleton v-if="detailLoading" animation />
      <template v-else-if="commandDetail">
        <a-descriptions :column="1" size="small" bordered>
          <a-descriptions-item :label="t('devicePlatform.commands.columns.command')">
            <span class="dp-monospace">{{ commandDetail.command.id }}</span>
          </a-descriptions-item>
          <a-descriptions-item :label="t('devicePlatform.common.status')">
            <a-tag :color="getBusinessStatusMeta('command', commandDetail.command.status).color">
              {{ getBusinessStatusMeta('command', commandDetail.command.status).label }}
            </a-tag>
          </a-descriptions-item>
          <a-descriptions-item :label="t('devicePlatform.commands.columns.policy')">
            {{ commandDetail.command.delivery_policy }}
          </a-descriptions-item>
          <a-descriptions-item :label="t('devicePlatform.commands.detail.corrected')">
            {{
              commandDetail.command.corrected
                ? t('devicePlatform.commands.detail.correctedYes')
                : t('devicePlatform.commands.detail.correctedNo')
            }}
          </a-descriptions-item>
        </a-descriptions>
        <h3 class="dp-panel-title">{{ t('devicePlatform.commands.detail.timeline') }}</h3>
        <pre class="dp-json">{{
          JSON.stringify({ attempts: commandDetail.attempts, events: commandDetail.events }, null, 2)
        }}</pre>
      </template>
    </a-drawer>
  </div>
</template>

<script lang="ts" setup>
  import { computed, onMounted, reactive, ref, watch } from 'vue';
  import { useI18n } from 'vue-i18n';
  import { Message } from '@arco-design/web-vue';
  import useLoading from '@/hooks/loading';
  import { getBusinessStatusMeta } from '@/utils/device-platform-status';
  import {
    CommandDetail,
    CommandRecord,
    DeviceRecord,
    ProjectRecord,
    createCommand,
    queryCommandDetail,
    queryCommands,
    queryDevices,
    queryProjects,
  } from '@/api/device-platform';
  import '../device-platform/workspace.less';

  defineOptions({ name: 'CommandsIndex' });

  const { t } = useI18n();
  const { loading, setLoading } = useLoading(false);
  const projects = ref<ProjectRecord[]>([]);
  const devices = ref<DeviceRecord[]>([]);
  const commands = ref<CommandRecord[]>([]);
  const selectedProjectId = ref('');
  const selectedDeviceId = ref('');
  const commandDetail = ref<CommandDetail>();
  const detailVisible = ref(false);
  const detailLoading = ref(false);
  const commandForm = reactive({ command_type: 'query_status', idempotency_key: '' });

  const activeStatuses = ['created', 'queued', 'sent', 'acked'];
  const failedStatuses = ['failed', 'timeout', 'cancelled', 'offline'];
  const activeCount = computed(() => commands.value.filter((command) => activeStatuses.includes(command.status)).length);
  const successCount = computed(() => commands.value.filter((command) => command.status === 'success').length);
  const failedCount = computed(() => commands.value.filter((command) => failedStatuses.includes(command.status)).length);
  const columns = computed(() => [
    { title: t('devicePlatform.commands.columns.command'), slotName: 'command', width: 260 },
    { title: t('devicePlatform.commands.columns.device'), slotName: 'device', width: 260 },
    { title: t('devicePlatform.commands.columns.policy'), dataIndex: 'delivery_policy', width: 160 },
    { title: t('devicePlatform.common.status'), slotName: 'commandStatus', width: 130 },
    { title: t('devicePlatform.common.createdAt'), dataIndex: 'created_at', ellipsis: true, tooltip: true, width: 190 },
    { title: t('devicePlatform.common.actions'), slotName: 'action', width: 100 },
  ]);

  const refreshProjectData = async () => {
    if (!selectedProjectId.value) {
      devices.value = [];
      commands.value = [];
      selectedDeviceId.value = '';
      commandDetail.value = undefined;
      return;
    }
    const [deviceRes, commandRes] = await Promise.all([
      queryDevices(selectedProjectId.value),
      queryCommands(selectedProjectId.value),
    ]);
    devices.value = deviceRes.data;
    commands.value = commandRes.data;
    if (!devices.value.some((device) => device.id === selectedDeviceId.value)) {
      selectedDeviceId.value = devices.value[0]?.id || '';
    }
  };

  const refresh = async () => {
    const projectRes = await queryProjects();
    projects.value = projectRes.data;
    if (!selectedProjectId.value && projects.value[0]) selectedProjectId.value = projects.value[0].id;
    await refreshProjectData();
  };

  watch(selectedProjectId, refreshProjectData);

  const loadCommandDetail = async (record: CommandRecord) => {
    detailVisible.value = true;
    detailLoading.value = true;
    commandDetail.value = undefined;
    try {
      const res = await queryCommandDetail(record.id, record.project_id || selectedProjectId.value);
      commandDetail.value = res.data;
    } finally {
      detailLoading.value = false;
    }
  };

  const handleCreateCommand = async () => {
    setLoading(true);
    try {
      const res = await createCommand({
        project_id: selectedProjectId.value,
        device_id: selectedDeviceId.value,
        command_type: commandForm.command_type,
        idempotency_key: commandForm.idempotency_key || `ui-${Date.now()}`,
      });
      await refreshProjectData();
      await loadCommandDetail(res.data);
      commandForm.idempotency_key = '';
      Message.success(t('devicePlatform.commands.message.sent'));
    } finally {
      setLoading(false);
    }
  };

  onMounted(refresh);
</script>
