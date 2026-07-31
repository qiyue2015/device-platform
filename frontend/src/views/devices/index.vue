<template>
  <div class="page-container device-platform-page">
    <section class="dp-overview">
      <div class="dp-overview-main">
        <div class="dp-kicker">{{ t('devicePlatform.kicker') }}</div>
        <h1 class="dp-title">{{ t('devicePlatform.devices.title') }}</h1>
        <p class="dp-description">{{ t('devicePlatform.devices.description') }}</p>
      </div>
      <div class="dp-overview-actions">
        <a-space>
          <a-button @click="refresh">
            <template #icon><icon-refresh /></template>
            {{ t('devicePlatform.common.refresh') }}
          </a-button>
          <a-button type="primary" data-testid="create-device" :disabled="!selectedProjectId" @click="openCreateModal">
            <template #icon><icon-plus /></template>
            {{ t('devicePlatform.devices.action.create') }}
          </a-button>
        </a-space>
      </div>
      <div class="dp-metrics">
        <div class="dp-metric">
          <div class="dp-metric-label">{{ t('devicePlatform.devices.metric.total') }}</div>
          <div class="dp-metric-value">{{ pagination.total }}</div>
        </div>
        <div class="dp-metric is-green">
          <div class="dp-metric-label">{{ t('devicePlatform.devices.metric.online') }}</div>
          <div class="dp-metric-value">{{ onlineCount }}</div>
        </div>
        <div class="dp-metric is-orange">
          <div class="dp-metric-label">{{ t('devicePlatform.devices.metric.offline') }}</div>
          <div class="dp-metric-value">{{ offlineCount }}</div>
        </div>
        <div class="dp-metric is-purple">
          <div class="dp-metric-label">{{ t('devicePlatform.devices.metric.cloud') }}</div>
          <div class="dp-metric-value">{{ cloudDeviceCount }}</div>
        </div>
      </div>
    </section>

    <Grid :title="t('devicePlatform.devices.table.title')">
      <GridToolbar @refresh="refresh">
        <template #prepend>
          <a-space wrap>
            <a-select
              v-model="selectedProjectId"
              class="dp-toolbar-control"
              :placeholder="t('devicePlatform.devices.filter.project')"
              :loading="projectOptions.loading"
              allow-clear
              allow-search
              @change="handleFilterChange"
              @dropdown-reach-bottom="loadMoreProjectOptions"
            >
              <a-option v-for="project in projects" :key="project.id" :value="project.id">{{ project.name }}</a-option>
            </a-select>
            <a-select
              v-model="providerFilter"
              class="dp-toolbar-control"
              :placeholder="t('devicePlatform.common.provider')"
              allow-clear
              @change="handleFilterChange"
            >
              <a-option v-for="provider in providers" :key="provider.code" :value="provider.code">
                {{ provider.name }} ({{ provider.code }})
              </a-option>
            </a-select>
          </a-space>
        </template>
      </GridToolbar>
      <GridTable
        class="dp-table"
        :loading="loading"
        :data="devices"
        :columns="columns"
        :pagination="pagination"
        :scroll="{ x: 1160 }"
        @page-change="onPageChange"
        @page-size-change="onPageSizeChange"
      >
        <template #device="{ record }">
          <div class="dp-cell-primary">{{ record.name }}</div>
          <div class="dp-cell-secondary dp-monospace">{{ record.id }}</div>
        </template>
        <template #access="{ record }">
          <div class="dp-tag-list">
            <a-tag color="arcoblue">{{ record.access_type }}</a-tag>
            <a-tag>{{ record.transport_protocol }}</a-tag>
          </div>
        </template>
        <template #provider="{ record }">
          <div class="dp-cell-primary">{{ record.provider_code }}</div>
          <div class="dp-cell-secondary dp-monospace">{{
            record.provider_device_id || t('devicePlatform.common.notConfigured')
          }}</div>
        </template>
        <template #connection="{ record }">
          <a-tag :color="getBusinessStatusMeta('connection', record.connection_status).color">
            {{ getBusinessStatusMeta('connection', record.connection_status).label }}
          </a-tag>
        </template>
        <template #lifecycle="{ record }">
          <a-tag :color="getBusinessStatusMeta('lifecycle', record.lifecycle_status).color">
            {{ getBusinessStatusMeta('lifecycle', record.lifecycle_status).label }}
          </a-tag>
        </template>
        <template #action="{ record }">
          <a-button type="text" size="small" :disabled="record.lifecycle_status === 'deleted'" @click="openEditModal(record)">
            {{ t('devicePlatform.devices.action.edit') }}
          </a-button>
        </template>
      </GridTable>
    </Grid>

    <a-modal
      v-model:visible="createVisible"
      :title="t('devicePlatform.devices.modal.title')"
      width="min(calc(100vw - 24px), 520px)"
      :ok-text="t('devicePlatform.devices.action.create')"
      :ok-loading="loading"
      @before-ok="handleCreateDevice"
    >
      <a-form :model="deviceForm" layout="vertical">
        <a-form-item :label="t('devicePlatform.common.project')">
          <a-select
            v-model="selectedProjectId"
            :placeholder="t('devicePlatform.devices.form.project.placeholder')"
            :loading="projectOptions.loading"
            allow-search
            @dropdown-reach-bottom="loadMoreProjectOptions"
          >
            <a-option v-for="project in projects" :key="project.id" :value="project.id">{{ project.name }}</a-option>
          </a-select>
        </a-form-item>
        <a-form-item :label="t('devicePlatform.devices.form.name')">
          <a-input
            v-model="deviceForm.name"
            data-testid="device-name"
            :placeholder="t('devicePlatform.devices.form.name.placeholder')"
          />
        </a-form-item>
        <a-form-item :label="t('devicePlatform.common.type')">
          <a-select v-model="deviceForm.device_type_code" data-testid="device-type-code">
            <a-option v-for="deviceType in deviceTypes" :key="deviceType.code" :value="deviceType.code">
              {{ deviceType.name }} ({{ deviceType.code }})
            </a-option>
          </a-select>
        </a-form-item>
        <a-form-item :label="t('devicePlatform.devices.form.provider')">
          <a-select v-model="deviceForm.provider_code" data-testid="device-provider-code">
            <a-option v-for="provider in providers" :key="provider.code" :value="provider.code">
              {{ provider.name }} ({{ provider.integration_status }})
            </a-option>
          </a-select>
        </a-form-item>
        <a-form-item :label="t('devicePlatform.devices.form.transportAdapter')">
          <a-input
            :model-value="deviceAccessPreset ? `${deviceAccessPreset.transport_protocol} / ${deviceAccessPreset.adapter}` : '-'"
            readonly
          />
        </a-form-item>
        <a-form-item v-if="deviceForm.provider_code === 'wwtiot'" :label="t('devicePlatform.devices.form.providerDeviceId')">
          <a-input
            v-model="deviceForm.provider_device_id"
            :placeholder="t('devicePlatform.devices.form.providerDeviceId.cloudPlaceholder')"
            data-testid="device-provider-device-id"
          />
        </a-form-item>
        <a-alert v-else>{{ t('devicePlatform.devices.form.simulatorIdentity') }}</a-alert>
      </a-form>
    </a-modal>

    <a-modal
      v-model:visible="editVisible"
      :title="t('devicePlatform.devices.edit.title')"
      width="min(calc(100vw - 24px), 520px)"
      :ok-loading="loading"
      @before-ok="handleUpdateDevice"
    >
      <a-form :model="editForm" layout="vertical">
        <a-form-item :label="t('devicePlatform.devices.form.name')">
          <a-input v-model="editForm.name" :disabled="editForm.lifecycle_status === 'deleted'" />
        </a-form-item>
        <a-form-item :label="t('devicePlatform.devices.columns.lifecycle')">
          <a-select v-model="editForm.lifecycle_status">
            <a-option v-for="status in editableLifecycleStatuses" :key="status" :value="status">{{ status }}</a-option>
          </a-select>
        </a-form-item>
        <a-alert v-if="editForm.lifecycle_status === 'deleted'" type="warning">
          {{ t('devicePlatform.devices.edit.deleteWarning') }}
        </a-alert>
      </a-form>
    </a-modal>
  </div>
</template>

<script lang="ts" setup>
  import { computed, onMounted, reactive, ref, watch } from 'vue';
  import { useI18n } from 'vue-i18n';
  import { Message } from '@arco-design/web-vue';
  import useLoading from '@/hooks/loading';
  import { getBusinessStatusMeta } from '@/utils/device-platform-status';
  import {
    CloudProviderRecord,
    DeviceRecord,
    DeviceTypeRecord,
    ProjectRecord,
    createDevice,
    queryCloudProviders,
    queryDeviceTypes,
    queryDevices,
    queryProjects,
    updateDevice,
  } from '@/api/device-platform';
  import '../device-platform/workspace.less';

  defineOptions({ name: 'DevicesIndex' });

  const { t } = useI18n();
  const { loading, setLoading } = useLoading(false);
  const projects = ref<ProjectRecord[]>([]);
  const providers = ref<CloudProviderRecord[]>([]);
  const deviceTypes = ref<DeviceTypeRecord[]>([]);
  const devices = ref<DeviceRecord[]>([]);
  const selectedProjectId = ref('');
  const providerFilter = ref('');
  const deviceListRequest = ref(0);
  const createVisible = ref(false);
  const editVisible = ref(false);
  const editingDevice = ref<DeviceRecord>();
  const deviceForm = reactive({
    name: '',
    device_type_code: 'smart-lock',
    provider_code: 'simulator',
    provider_device_id: '',
  });
  const editForm = reactive({ name: '', lifecycle_status: 'active' });
  const pagination = reactive({
    current: 1,
    pageSize: 10,
    total: 0,
    showTotal: true,
    showPageSize: true,
  });
  const projectOptions = reactive({ page: 1, total: 0, loaded: 0, loading: false, request: 0 });

  const deviceAccessPreset = computed(
    () => providers.value.find((provider) => provider.code === deviceForm.provider_code) || providers.value[0]
  );

  const onlineCount = computed(() => devices.value.filter((device) => device.connection_status === 'online').length);
  const offlineCount = computed(() => devices.value.filter((device) => device.connection_status === 'offline').length);
  const cloudDeviceCount = computed(() => devices.value.filter((device) => device.access_type === 'cloud_api').length);
  const columns = computed(() => [
    { title: t('devicePlatform.devices.columns.name'), slotName: 'device', width: 260 },
    { title: t('devicePlatform.common.access'), slotName: 'access', width: 180 },
    { title: t('devicePlatform.devices.columns.providerDevice'), slotName: 'provider', width: 260 },
    { title: t('devicePlatform.common.adapter'), dataIndex: 'adapter', ellipsis: true, tooltip: true, width: 220 },
    { title: t('devicePlatform.devices.columns.connection'), slotName: 'connection', width: 120 },
    { title: t('devicePlatform.devices.columns.lifecycle'), slotName: 'lifecycle', width: 120 },
    { title: t('devicePlatform.common.actions'), slotName: 'action', width: 100 },
  ]);
  const editableLifecycleStatuses = computed(() =>
    editingDevice.value?.lifecycle_status === 'disabled' ? ['disabled', 'active', 'deleted'] : ['active', 'disabled', 'deleted']
  );

  const refreshDevices = async () => {
    deviceListRequest.value += 1;
    const { value: request } = deviceListRequest;
    if (!selectedProjectId.value) {
      devices.value = [];
      pagination.total = 0;
      return;
    }
    const projectId = selectedProjectId.value;
    const providerCode = providerFilter.value || undefined;
    const { current: page, pageSize } = pagination;
    const deviceRes = await queryDevices({
      project_id: projectId,
      provider_code: providerCode,
      page,
      page_size: pageSize,
    });
    if (
      request !== deviceListRequest.value ||
      projectId !== selectedProjectId.value ||
      providerCode !== (providerFilter.value || undefined) ||
      page !== pagination.current ||
      pageSize !== pagination.pageSize
    )
      return;
    devices.value = deviceRes.data;
    pagination.total = deviceRes.meta?.total ?? deviceRes.data.length;
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

  const refresh = async () => {
    const [, providerRes, deviceTypeRes] = await Promise.all([
      loadProjectOptions(true),
      queryCloudProviders({ page: 1, page_size: 100 }),
      queryDeviceTypes({ page: 1, page_size: 100 }),
    ]);
    providers.value = providerRes.data;
    deviceTypes.value = deviceTypeRes.data;
    if (!selectedProjectId.value && projects.value[0]) selectedProjectId.value = projects.value[0].id;
    if (!providers.value.some((provider) => provider.code === deviceForm.provider_code)) {
      deviceForm.provider_code = providers.value[0]?.code || '';
    }
    if (!deviceTypes.value.some((deviceType) => deviceType.code === deviceForm.device_type_code)) {
      deviceForm.device_type_code = deviceTypes.value[0]?.code || '';
    }
    await refreshDevices();
  };

  watch(
    () => deviceForm.provider_code,
    (providerCode) => {
      if (providerCode !== 'wwtiot') deviceForm.provider_device_id = '';
    }
  );

  const handleFilterChange = () => {
    pagination.current = 1;
    refreshDevices();
  };

  const onPageChange = (page: number) => {
    pagination.current = page;
    refreshDevices();
  };

  const onPageSizeChange = (pageSize: number) => {
    pagination.pageSize = pageSize;
    pagination.current = 1;
    refreshDevices();
  };

  const openCreateModal = () => {
    createVisible.value = true;
  };

  const openEditModal = (record: DeviceRecord) => {
    editingDevice.value = record;
    editForm.name = record.name;
    editForm.lifecycle_status = record.lifecycle_status;
    editVisible.value = true;
  };

  const handleCreateDevice = async (done: (closed: boolean) => void) => {
    if (!deviceForm.name.trim()) {
      Message.warning(t('devicePlatform.devices.message.nameRequired'));
      done(false);
      return;
    }
    if (deviceForm.provider_code === 'wwtiot' && !deviceForm.provider_device_id.trim()) {
      Message.warning(t('devicePlatform.devices.message.providerDeviceRequired'));
      done(false);
      return;
    }
    setLoading(true);
    try {
      await createDevice({
        project_id: selectedProjectId.value,
        name: deviceForm.name.trim(),
        device_type_code: deviceForm.device_type_code,
        provider_code: deviceForm.provider_code,
        ...(deviceForm.provider_code === 'wwtiot' ? { provider_device_id: deviceForm.provider_device_id.trim() } : {}),
      });
      deviceForm.name = '';
      deviceForm.provider_device_id = '';
      createVisible.value = false;
      pagination.current = 1;
      await refresh();
      Message.success(t('devicePlatform.devices.message.created'));
      done(true);
    } catch {
      done(false);
    } finally {
      setLoading(false);
    }
  };

  const handleUpdateDevice = async (done: (closed: boolean) => void) => {
    if (!editingDevice.value) {
      done(false);
      return;
    }
    if (editForm.lifecycle_status !== 'deleted' && !editForm.name.trim()) {
      Message.warning(t('devicePlatform.devices.message.nameRequired'));
      done(false);
      return;
    }
    setLoading(true);
    try {
      const data: { name?: string; lifecycle_status?: string } = {};
      if (editForm.lifecycle_status === 'deleted') {
        data.lifecycle_status = 'deleted';
      } else {
        const name = editForm.name.trim();
        if (name !== editingDevice.value.name) data.name = name;
        if (editForm.lifecycle_status !== editingDevice.value.lifecycle_status) {
          data.lifecycle_status = editForm.lifecycle_status;
        }
      }
      if (Object.keys(data).length === 0) {
        editVisible.value = false;
        done(true);
        return;
      }
      await updateDevice(editingDevice.value.id, data);
      await refreshDevices();
      editVisible.value = false;
      Message.success(t('devicePlatform.devices.message.updated'));
      done(true);
    } catch {
      done(false);
    } finally {
      setLoading(false);
    }
  };

  onMounted(refresh);
</script>
