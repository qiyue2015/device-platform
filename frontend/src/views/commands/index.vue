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
          <div class="dp-metric-value">{{ pagination.total }}</div>
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
          <a-space class="commands-toolbar-fields" wrap>
            <a-select
              v-model="selectedProjectId"
              class="dp-toolbar-control"
              :placeholder="t('devicePlatform.commands.filter.project')"
              :loading="projectOptions.loading"
              allow-search
              @change="handleProjectChange"
              @dropdown-reach-bottom="loadMoreProjectOptions"
            >
              <a-option v-for="project in projects" :key="project.id" :value="project.id">{{ project.name }}</a-option>
            </a-select>
            <a-select
              v-model="selectedDeviceId"
              class="dp-toolbar-control"
              :placeholder="t('devicePlatform.commands.filter.device')"
              :loading="deviceOptions.loading"
              allow-search
              @dropdown-reach-bottom="loadMoreDeviceOptions"
            >
              <a-option v-for="device in devices" :key="device.id" :value="device.id">
                {{ device.name }} · {{ device.provider_device_id }}
              </a-option>
            </a-select>
            <a-select v-model="commandForm.command_type" class="dp-toolbar-control" data-testid="command-type">
              <a-option v-for="action in commandActions" :key="action" :value="action">{{ action }}</a-option>
            </a-select>
            <a-input
              v-model="commandForm.idempotency_key"
              class="dp-toolbar-control is-wide"
              :placeholder="t('devicePlatform.commands.form.idempotency.placeholder')"
            />
            <a-button
              type="primary"
              data-testid="send-command"
              :disabled="!selectedDeviceId || selectedDevice?.lifecycle_status !== 'active' || !commandForm.command_type"
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
        :pagination="pagination"
        :scroll="{ x: 1100 }"
        @page-change="onPageChange"
        @page-size-change="onPageSizeChange"
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
          <a-space>
            <a-button type="text" size="small" @click="loadCommandDetail(record)">
              {{ t('devicePlatform.commands.action.detail') }}
            </a-button>
            <a-popconfirm
              :content="t('devicePlatform.commands.confirm.cancel')"
              :disabled="record.status !== 'queued'"
              @ok="handleCancelCommand(record)"
            >
              <a-button type="text" size="small" :disabled="record.status !== 'queued'">
                {{ t('devicePlatform.commands.action.cancel') }}
              </a-button>
            </a-popconfirm>
          </a-space>
        </template>
      </GridTable>
    </Grid>

    <a-drawer
      v-model:visible="detailVisible"
      :title="t('devicePlatform.commands.drawer.title')"
      :footer="false"
      width="min(100vw, 520px)"
    >
      <a-skeleton v-if="detailLoading" animation />
      <template v-else-if="commandDetail">
        <a-descriptions :column="1" size="small" bordered>
          <a-descriptions-item :label="t('devicePlatform.commands.columns.command')">
            <span class="dp-monospace">{{ commandDetail.id }}</span>
          </a-descriptions-item>
          <a-descriptions-item :label="t('devicePlatform.common.status')">
            <a-tag :color="getBusinessStatusMeta('command', commandDetail.status).color">
              {{ getBusinessStatusMeta('command', commandDetail.status).label }}
            </a-tag>
          </a-descriptions-item>
          <a-descriptions-item :label="t('devicePlatform.commands.columns.policy')">
            {{ commandDetail.delivery_policy }}
          </a-descriptions-item>
          <a-descriptions-item :label="t('devicePlatform.commands.detail.confirmation')">
            {{ commandDetail.confirmation_level }} / {{ commandDetail.evidence_status }}
          </a-descriptions-item>
          <a-descriptions-item :label="t('devicePlatform.commands.detail.reason')">
            {{ commandDetail.reason_code || '-'
            }}<span v-if="commandDetail.reason_detail"> · {{ commandDetail.reason_detail }}</span>
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
    DeviceTypeRecord,
    ProjectRecord,
    cancelCommand,
    createCommand,
    queryCommandDetail,
    queryCommands,
    queryDevices,
    queryDeviceTypes,
    queryProjects,
  } from '@/api/device-platform';
  import '../device-platform/workspace.less';

  defineOptions({ name: 'CommandsIndex' });

  const { t } = useI18n();
  const { loading, setLoading } = useLoading(false);
  const projects = ref<ProjectRecord[]>([]);
  const devices = ref<DeviceRecord[]>([]);
  const deviceTypes = ref<DeviceTypeRecord[]>([]);
  const commands = ref<CommandRecord[]>([]);
  const selectedProjectId = ref('');
  const selectedDeviceId = ref('');
  const commandListRequest = ref(0);
  const commandDetail = ref<CommandDetail>();
  const detailVisible = ref(false);
  const detailLoading = ref(false);
  const commandForm = reactive({ command_type: 'query_status', idempotency_key: '' });
  const pagination = reactive({
    current: 1,
    pageSize: 10,
    total: 0,
    showTotal: true,
    showPageSize: true,
  });
  const projectOptions = reactive({ page: 1, total: 0, loaded: 0, loading: false, request: 0 });
  const deviceOptions = reactive({ page: 1, total: 0, loaded: 0, loading: false, request: 0 });

  const activeStatuses = ['queued', 'sent', 'acked'];
  const failedStatuses = ['failed', 'timeout', 'cancelled', 'unknown'];
  const activeCount = computed(() => commands.value.filter((command) => activeStatuses.includes(command.status)).length);
  const successCount = computed(() => commands.value.filter((command) => command.status === 'success').length);
  const failedCount = computed(() => commands.value.filter((command) => failedStatuses.includes(command.status)).length);
  const columns = computed(() => [
    { title: t('devicePlatform.commands.columns.command'), slotName: 'command', width: 260 },
    { title: t('devicePlatform.commands.columns.device'), slotName: 'device', width: 260 },
    { title: t('devicePlatform.commands.columns.policy'), dataIndex: 'delivery_policy', width: 160 },
    { title: t('devicePlatform.common.status'), slotName: 'commandStatus', width: 130 },
    { title: t('devicePlatform.common.createdAt'), dataIndex: 'created_at', ellipsis: true, tooltip: true, width: 190 },
    { title: t('devicePlatform.common.actions'), slotName: 'action', width: 170 },
  ]);

  const selectedDevice = computed(() => devices.value.find((device) => device.id === selectedDeviceId.value));
  const commandActions = computed(
    () =>
      deviceTypes.value
        .find((deviceType) => deviceType.code === selectedDevice.value?.device_type_code)
        ?.actions.map((action) => action.identifier) || []
  );

  const refreshCommands = async () => {
    commandListRequest.value += 1;
    const { value: request } = commandListRequest;
    if (!selectedProjectId.value) {
      commands.value = [];
      pagination.total = 0;
      commandDetail.value = undefined;
      return;
    }
    const projectId = selectedProjectId.value;
    const { current: page, pageSize } = pagination;
    const commandRes = await queryCommands({
      project_id: projectId,
      page,
      page_size: pageSize,
    });
    if (
      request !== commandListRequest.value ||
      projectId !== selectedProjectId.value ||
      page !== pagination.current ||
      pageSize !== pagination.pageSize
    )
      return;
    commands.value = commandRes.data;
    pagination.total = commandRes.meta?.total ?? commandRes.data.length;
  };

  const loadProjectOptions = async (reset = false) => {
    if (projectOptions.loading && !reset) return;
    if (!reset && projectOptions.loaded >= projectOptions.total) return;

    const selected = projects.value.find((project) => project.id === selectedProjectId.value);
    if (reset) projectOptions.request += 1;
    const { request } = projectOptions;
    const page = reset ? 1 : projectOptions.page;
    if (reset) {
      projectOptions.page = 1;
      projectOptions.total = 0;
      projectOptions.loaded = 0;
    }
    projectOptions.loading = true;
    try {
      const res = await queryProjects({ page, page_size: 100 });
      if (request !== projectOptions.request) return;
      const incoming =
        reset && selected && !res.data.some((project) => project.id === selected.id) ? [selected, ...res.data] : res.data;
      const known = new Set(projects.value.map((project) => project.id));
      projects.value = reset ? incoming : [...projects.value, ...incoming.filter((project) => !known.has(project.id))];
      projectOptions.loaded = reset ? res.data.length : projectOptions.loaded + res.data.length;
      projectOptions.total = res.meta?.total ?? projectOptions.loaded;
      projectOptions.page = page + 1;
    } finally {
      if (request === projectOptions.request) projectOptions.loading = false;
    }
  };

  const loadMoreProjectOptions = () => loadProjectOptions();

  const loadDeviceOptions = async (reset = false) => {
    if (!selectedProjectId.value) {
      deviceOptions.request += 1;
      devices.value = [];
      selectedDeviceId.value = '';
      deviceOptions.total = 0;
      deviceOptions.loaded = 0;
      deviceOptions.loading = false;
      return;
    }
    if (deviceOptions.loading && !reset) return;
    if (!reset && deviceOptions.loaded >= deviceOptions.total) return;

    const selected = devices.value.find(
      (device) => device.id === selectedDeviceId.value && device.project_id === selectedProjectId.value
    );
    if (reset) deviceOptions.request += 1;
    const { request } = deviceOptions;
    const page = reset ? 1 : deviceOptions.page;
    if (reset) {
      deviceOptions.page = 1;
      deviceOptions.total = 0;
      deviceOptions.loaded = 0;
    }
    deviceOptions.loading = true;
    try {
      const res = await queryDevices({ project_id: selectedProjectId.value, page, page_size: 100 });
      if (request !== deviceOptions.request) return;
      const incoming =
        reset && selected && !res.data.some((device) => device.id === selected.id) ? [selected, ...res.data] : res.data;
      const known = new Set(devices.value.map((device) => device.id));
      devices.value = reset ? incoming : [...devices.value, ...incoming.filter((device) => !known.has(device.id))];
      deviceOptions.loaded = reset ? res.data.length : deviceOptions.loaded + res.data.length;
      deviceOptions.total = res.meta?.total ?? deviceOptions.loaded;
      deviceOptions.page = page + 1;
    } finally {
      if (request === deviceOptions.request) deviceOptions.loading = false;
    }
  };

  const loadMoreDeviceOptions = () => loadDeviceOptions();

  const refreshProjectData = async () => {
    if (!selectedProjectId.value) {
      await loadDeviceOptions(true);
      await refreshCommands();
      return;
    }
    const projectId = selectedProjectId.value;
    await loadDeviceOptions(true);
    if (projectId !== selectedProjectId.value) return;
    if (!devices.value.some((device) => device.id === selectedDeviceId.value)) {
      selectedDeviceId.value = devices.value[0]?.id || '';
    }
    await refreshCommands();
  };

  const refresh = async () => {
    const [, deviceTypeRes] = await Promise.all([loadProjectOptions(true), queryDeviceTypes({ page: 1, page_size: 100 })]);
    deviceTypes.value = deviceTypeRes.data;
    if (!selectedProjectId.value && projects.value[0]) selectedProjectId.value = projects.value[0].id;
    await refreshProjectData();
  };

  watch(commandActions, (actions) => {
    if (!actions.includes(commandForm.command_type)) {
      commandForm.command_type = actions[0] || '';
    }
  });

  const handleProjectChange = () => {
    selectedDeviceId.value = '';
    pagination.current = 1;
    refreshProjectData();
  };

  const onPageChange = (page: number) => {
    pagination.current = page;
    refreshCommands();
  };

  const onPageSizeChange = (pageSize: number) => {
    pagination.pageSize = pageSize;
    pagination.current = 1;
    refreshCommands();
  };

  const loadCommandDetail = async (record: CommandRecord) => {
    detailVisible.value = true;
    detailLoading.value = true;
    commandDetail.value = undefined;
    try {
      const res = await queryCommandDetail(record.id);
      commandDetail.value = res.data;
    } finally {
      detailLoading.value = false;
    }
  };

  const handleCancelCommand = async (record: CommandRecord) => {
    setLoading(true);
    try {
      await cancelCommand(record.id);
      await refreshCommands();
      Message.success(t('devicePlatform.commands.message.cancelled'));
    } finally {
      setLoading(false);
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
      await refreshCommands();
      await loadCommandDetail(res.data);
      commandForm.idempotency_key = '';
      Message.success(t('devicePlatform.commands.message.sent'));
    } finally {
      setLoading(false);
    }
  };

  onMounted(refresh);
</script>

<style lang="less" scoped>
  @media (width <= 768px) {
    .commands-toolbar-fields {
      display: grid;
      grid-template-columns: minmax(0, 1fr);
      width: 100%;

      :deep(> .arco-space-item) {
        width: 100%;
        margin-right: 0 !important;
      }

      :deep(.arco-btn) {
        width: 100%;
      }
    }
  }
</style>
