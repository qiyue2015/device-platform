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
          <div class="dp-metric-value">{{ devices.length }}</div>
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
              allow-clear
            >
              <a-option v-for="project in projects" :key="project.id" :value="project.id">{{ project.name }}</a-option>
            </a-select>
            <a-select
              v-model="accessFilter"
              class="dp-toolbar-control"
              :placeholder="t('devicePlatform.devices.filter.access')"
              allow-clear
            >
              <a-option value="mock_gateway">mock_gateway</a-option>
              <a-option value="cloud_api">cloud_api</a-option>
            </a-select>
          </a-space>
        </template>
      </GridToolbar>
      <GridTable
        class="dp-table"
        :loading="loading"
        :data="filteredDevices"
        :columns="columns"
        :pagination="{ pageSize: 10 }"
        :scroll="{ x: 1160 }"
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
      </GridTable>
    </Grid>

    <a-modal
      v-model:visible="createVisible"
      :title="t('devicePlatform.devices.modal.title')"
      :ok-text="t('devicePlatform.devices.action.create')"
      :ok-loading="loading"
      @before-ok="handleCreateDevice"
    >
      <a-form :model="deviceForm" layout="vertical">
        <a-form-item :label="t('devicePlatform.common.project')">
          <a-select v-model="selectedProjectId" :placeholder="t('devicePlatform.devices.form.project.placeholder')">
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
        <a-form-item :label="t('devicePlatform.devices.form.access')">
          <a-select v-model="deviceForm.access_type" data-testid="device-access-type">
            <a-option value="mock_gateway">mock_gateway</a-option>
            <a-option value="cloud_api">cloud_api</a-option>
          </a-select>
        </a-form-item>
        <a-form-item :label="t('devicePlatform.devices.form.provider')">
          <a-select
            v-if="deviceForm.access_type === 'cloud_api'"
            v-model="deviceForm.provider_code"
            data-testid="device-provider-code"
          >
            <a-option v-for="provider in cloudProviders" :key="provider.code" :value="provider.code">
              {{ provider.name }} ({{ provider.code }})
            </a-option>
          </a-select>
          <a-input v-else :model-value="deviceAccessPreset.provider_code" readonly />
        </a-form-item>
        <a-form-item :label="t('devicePlatform.devices.form.transportAdapter')">
          <a-input :model-value="`${deviceAccessPreset.transport_protocol} / ${deviceAccessPreset.adapter}`" readonly />
        </a-form-item>
        <a-form-item :label="t('devicePlatform.devices.form.providerDeviceId')">
          <a-input
            v-model="deviceForm.provider_device_id"
            :placeholder="providerDevicePlaceholder"
            data-testid="device-provider-device-id"
          />
        </a-form-item>
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
    ProjectRecord,
    createDevice,
    queryCloudProviders,
    queryDevices,
    queryProjects,
  } from '@/api/device-platform';
  import '../device-platform/workspace.less';

  defineOptions({ name: 'DevicesIndex' });

  const { t } = useI18n();
  const { loading, setLoading } = useLoading(false);
  const projects = ref<ProjectRecord[]>([]);
  const cloudProviders = ref<CloudProviderRecord[]>([]);
  const devices = ref<DeviceRecord[]>([]);
  const selectedProjectId = ref('');
  const accessFilter = ref('');
  const createVisible = ref(false);
  const deviceForm = reactive({
    name: '',
    access_type: 'mock_gateway',
    provider_code: '',
    provider_device_id: '',
  });

  const accessPresets: Record<string, { provider_code: string; transport_protocol: string; adapter: string }> = {
    mock_gateway: {
      provider_code: 'simulator',
      transport_protocol: 'simulator',
      adapter: 'mock_gateway',
    },
    cloud_api: {
      provider_code: 'wwtiot',
      transport_protocol: 'http',
      adapter: 'wwtiot_cloud_api',
    },
  };

  const selectedCloudProvider = computed(() =>
    cloudProviders.value.find((provider) => provider.code === deviceForm.provider_code)
  );

  const deviceAccessPreset = computed(() => {
    if (deviceForm.access_type === 'cloud_api' && selectedCloudProvider.value) {
      return {
        provider_code: selectedCloudProvider.value.code,
        transport_protocol: selectedCloudProvider.value.transport_protocol,
        adapter: selectedCloudProvider.value.adapter,
      };
    }
    return accessPresets[deviceForm.access_type] || accessPresets.mock_gateway;
  });

  const providerDevicePlaceholder = computed(() =>
    deviceForm.access_type === 'cloud_api'
      ? t('devicePlatform.devices.form.providerDeviceId.cloudPlaceholder')
      : t('devicePlatform.devices.form.providerDeviceId.simulatorPlaceholder')
  );

  const filteredDevices = computed(() =>
    accessFilter.value ? devices.value.filter((device) => device.access_type === accessFilter.value) : devices.value
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
  ]);

  const refreshDevices = async () => {
    if (!selectedProjectId.value) {
      devices.value = [];
      return;
    }
    const deviceRes = await queryDevices(selectedProjectId.value);
    devices.value = deviceRes.data;
  };

  const refresh = async () => {
    const [projectRes, providerRes] = await Promise.all([queryProjects(), queryCloudProviders()]);
    projects.value = projectRes.data;
    cloudProviders.value = providerRes.data.filter((provider) => provider.access_type === 'cloud_api');
    if (!selectedProjectId.value && projects.value[0]) selectedProjectId.value = projects.value[0].id;
    if (!deviceForm.provider_code && cloudProviders.value[0]) deviceForm.provider_code = cloudProviders.value[0].code;
    await refreshDevices();
  };

  watch(selectedProjectId, refreshDevices);
  watch(
    () => deviceForm.access_type,
    (accessType) => {
      if (accessType === 'cloud_api' && !deviceForm.provider_code && cloudProviders.value[0]) {
        deviceForm.provider_code = cloudProviders.value[0].code;
      }
    }
  );

  const openCreateModal = () => {
    createVisible.value = true;
  };

  const handleCreateDevice = async (done: (closed: boolean) => void) => {
    if (!deviceForm.name.trim()) {
      Message.warning(t('devicePlatform.devices.message.nameRequired'));
      done(false);
      return;
    }
    if (deviceForm.access_type === 'cloud_api' && !deviceForm.provider_device_id.trim()) {
      Message.warning(t('devicePlatform.devices.message.providerDeviceRequired'));
      done(false);
      return;
    }
    setLoading(true);
    try {
      await createDevice({
        project_id: selectedProjectId.value,
        name: deviceForm.name.trim(),
        device_type: 'smart_lock',
        access_type: deviceForm.access_type,
        provider_device_id: deviceForm.provider_device_id,
        provider_code: deviceAccessPreset.value.provider_code,
        transport_protocol: deviceAccessPreset.value.transport_protocol,
        adapter: deviceAccessPreset.value.adapter,
      });
      deviceForm.name = '';
      deviceForm.provider_device_id = '';
      createVisible.value = false;
      await refresh();
      Message.success(t('devicePlatform.devices.message.created'));
      done(true);
    } catch {
      done(false);
    } finally {
      setLoading(false);
    }
  };

  onMounted(refresh);
</script>
