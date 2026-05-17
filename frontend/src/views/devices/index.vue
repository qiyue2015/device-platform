<template>
  <div class="page-container">
    <a-card :title="$t('menu.devices')" :bordered="false">
      <a-row :gutter="16">
        <a-col :span="8">
          <a-form :model="deviceForm" layout="vertical">
            <a-form-item label="Project">
              <a-select v-model="selectedProjectId">
                <a-option v-for="project in projects" :key="project.id" :value="project.id">{{ project.name }}</a-option>
              </a-select>
            </a-form-item>
            <a-form-item label="Name">
              <a-input v-model="deviceForm.name" data-testid="device-name" />
            </a-form-item>
            <a-form-item label="Access type">
              <a-select v-model="deviceForm.access_type" data-testid="device-access-type">
                <a-option value="mock_gateway">mock_gateway</a-option>
                <a-option value="cloud_api">cloud_api</a-option>
              </a-select>
            </a-form-item>
            <a-form-item label="Provider">
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
            <a-form-item label="Transport / Adapter">
              <a-input :model-value="`${deviceAccessPreset.transport_protocol} / ${deviceAccessPreset.adapter}`" readonly />
            </a-form-item>
            <a-form-item label="Provider device ID">
              <a-input
                v-model="deviceForm.provider_device_id"
                :placeholder="providerDevicePlaceholder"
                data-testid="device-provider-device-id"
              />
            </a-form-item>
            <a-button
              type="primary"
              long
              data-testid="create-device"
              :disabled="!selectedProjectId"
              :loading="loading"
              @click="handleCreateDevice"
            >
              <template #icon><icon-plus /></template>
              Create Device
            </a-button>
          </a-form>
        </a-col>
        <a-col :span="16">
          <a-table row-key="id" :loading="loading" :data="devices" :columns="columns">
            <template #status="{ record }">
              <a-tag :color="getBusinessStatusMeta('connection', record.connection_status).color">
                {{ getBusinessStatusMeta('connection', record.connection_status).label }}
              </a-tag>
            </template>
          </a-table>
        </a-col>
      </a-row>
    </a-card>
  </div>
</template>

<script lang="ts" setup>
  import { computed, onMounted, reactive, ref, watch } from 'vue';
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

  defineOptions({ name: 'DevicesIndex' });

  const { loading, setLoading } = useLoading(false);
  const projects = ref<ProjectRecord[]>([]);
  const cloudProviders = ref<CloudProviderRecord[]>([]);
  const devices = ref<DeviceRecord[]>([]);
  const selectedProjectId = ref('');
  const deviceForm = reactive({
    name: 'Smoke Test Lock',
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
    deviceForm.access_type === 'cloud_api' ? 'WWTIOT deviceid' : 'Simulator device ID'
  );

  const columns = computed(() => [
    { title: 'Name', dataIndex: 'name' },
    { title: 'Project', dataIndex: 'project_id', ellipsis: true, tooltip: true },
    { title: 'Access', dataIndex: 'access_type' },
    { title: 'Provider', dataIndex: 'provider_code' },
    { title: 'Provider Device', dataIndex: 'provider_device_id' },
    { title: 'Adapter', dataIndex: 'adapter' },
    { title: 'Status', slotName: 'status' },
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

  const handleCreateDevice = async () => {
    if (deviceForm.access_type === 'cloud_api' && !deviceForm.provider_device_id.trim()) {
      Message.warning('Provider device ID is required');
      return;
    }
    setLoading(true);
    try {
      await createDevice({
        project_id: selectedProjectId.value,
        name: deviceForm.name,
        device_type: 'smart_lock',
        access_type: deviceForm.access_type,
        provider_device_id: deviceForm.provider_device_id,
        provider_code: deviceAccessPreset.value.provider_code,
        transport_protocol: deviceAccessPreset.value.transport_protocol,
        adapter: deviceAccessPreset.value.adapter,
      });
      await refresh();
      Message.success('Device created');
    } finally {
      setLoading(false);
    }
  };

  onMounted(refresh);
</script>
