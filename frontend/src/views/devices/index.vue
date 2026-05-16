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
              </a-select>
            </a-form-item>
            <a-form-item label="Provider device ID">
              <a-input v-model="deviceForm.provider_device_id" data-testid="device-provider-device-id" />
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
              <a-tag :color="record.connection_status === 'online' ? 'green' : 'orange'">{{ record.connection_status }}</a-tag>
            </template>
          </a-table>
        </a-col>
      </a-row>
    </a-card>
  </div>
</template>

<script lang="ts" setup>
  import { computed, onMounted, reactive, ref } from 'vue';
  import { Message } from '@arco-design/web-vue';
  import useLoading from '@/hooks/loading';
  import { DeviceRecord, ProjectRecord, createDevice, queryDevices, queryProjects } from '@/api/device-platform';

  defineOptions({ name: 'DevicesIndex' });

  const { loading, setLoading } = useLoading(false);
  const projects = ref<ProjectRecord[]>([]);
  const devices = ref<DeviceRecord[]>([]);
  const selectedProjectId = ref('');
  const deviceForm = reactive({
    name: 'Smoke Test Lock',
    access_type: 'mock_gateway',
    provider_device_id: '',
  });

  const columns = computed(() => [
    { title: 'Name', dataIndex: 'name' },
    { title: 'Project', dataIndex: 'project_id', ellipsis: true, tooltip: true },
    { title: 'Access', dataIndex: 'access_type' },
    { title: 'Provider', dataIndex: 'provider_code' },
    { title: 'Provider Device', dataIndex: 'provider_device_id' },
    { title: 'Status', slotName: 'status' },
  ]);

  const refresh = async () => {
    const [projectRes, deviceRes] = await Promise.all([queryProjects(), queryDevices()]);
    projects.value = projectRes.data;
    devices.value = deviceRes.data;
    if (!selectedProjectId.value && projects.value[0]) selectedProjectId.value = projects.value[0].id;
  };

  const handleCreateDevice = async () => {
    setLoading(true);
    try {
      await createDevice({
        project_id: selectedProjectId.value,
        name: deviceForm.name,
        device_type: 'smart_lock',
        access_type: deviceForm.access_type,
        provider_device_id: deviceForm.provider_device_id,
        provider_code: 'simulator',
        transport_protocol: 'simulator',
        adapter: 'mock_gateway',
      });
      await refresh();
      Message.success('Device created');
    } finally {
      setLoading(false);
    }
  };

  onMounted(refresh);
</script>
