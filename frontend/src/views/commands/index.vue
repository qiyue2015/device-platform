<template>
  <div class="page-container">
    <a-card :title="$t('menu.commands')" :bordered="false">
      <a-space direction="vertical" fill>
        <a-space wrap>
          <a-select v-model="selectedProjectId" placeholder="Project" style="width: 220px">
            <a-option v-for="project in projects" :key="project.id" :value="project.id">{{ project.name }}</a-option>
          </a-select>
          <a-select v-model="selectedDeviceId" placeholder="Device" style="width: 220px">
            <a-option v-for="device in devices" :key="device.id" :value="device.id">{{ device.name }}</a-option>
          </a-select>
          <a-select v-model="commandForm.command_type" data-testid="command-type" style="width: 160px">
            <a-option value="unlock">unlock</a-option>
            <a-option value="lock">lock</a-option>
            <a-option value="query_status">query_status</a-option>
            <a-option value="set_config">set_config</a-option>
            <a-option value="reboot">reboot</a-option>
          </a-select>
          <a-input v-model="commandForm.idempotency_key" placeholder="idempotency key" style="width: 220px" />
          <a-button
            type="primary"
            data-testid="send-command"
            :disabled="!selectedDeviceId"
            :loading="loading"
            @click="handleCreateCommand"
          >
            <template #icon><icon-send /></template>
            Send
          </a-button>
        </a-space>
        <a-table
          row-key="id"
          :pagination="{ pageSize: 8 }"
          :loading="loading"
          :data="commands"
          :columns="columns"
          @row-click="loadCommandDetail"
        >
          <template #commandStatus="{ record }">
            <a-tag :color="statusColor(record.status)">{{ record.status }}</a-tag>
          </template>
        </a-table>
        <a-card v-if="commandDetail" title="Command Detail" :bordered="false">
          <a-descriptions :column="2" size="small" bordered>
            <a-descriptions-item label="Command">{{ commandDetail.command.id }}</a-descriptions-item>
            <a-descriptions-item label="Status">{{ commandDetail.command.status }}</a-descriptions-item>
            <a-descriptions-item label="Policy">{{ commandDetail.command.delivery_policy }}</a-descriptions-item>
            <a-descriptions-item label="Corrected">{{ commandDetail.command.corrected ? 'yes' : 'no' }}</a-descriptions-item>
          </a-descriptions>
          <pre>{{ JSON.stringify({ attempts: commandDetail.attempts, events: commandDetail.events }, null, 2) }}</pre>
        </a-card>
      </a-space>
    </a-card>
  </div>
</template>

<script lang="ts" setup>
  import { computed, onMounted, reactive, ref, watch } from 'vue';
  import { Message } from '@arco-design/web-vue';
  import useLoading from '@/hooks/loading';
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

  defineOptions({ name: 'CommandsIndex' });

  const { loading, setLoading } = useLoading(false);
  const projects = ref<ProjectRecord[]>([]);
  const devices = ref<DeviceRecord[]>([]);
  const commands = ref<CommandRecord[]>([]);
  const selectedProjectId = ref('');
  const selectedDeviceId = ref('');
  const commandDetail = ref<CommandDetail>();
  const commandForm = reactive({ command_type: 'query_status', idempotency_key: '' });

  const columns = computed(() => [
    { title: 'ID', dataIndex: 'id', ellipsis: true, tooltip: true },
    { title: 'Device', dataIndex: 'device_id', ellipsis: true, tooltip: true },
    { title: 'Type', dataIndex: 'command_type' },
    { title: 'Policy', dataIndex: 'delivery_policy' },
    { title: 'Status', slotName: 'commandStatus' },
  ]);

  const statusColor = (status: string) => {
    if (['success', 'delivered', 'online'].includes(status)) return 'green';
    if (['failed', 'dead', 'timeout'].includes(status)) return 'red';
    if (['offline', 'pending'].includes(status)) return 'orange';
    return 'arcoblue';
  };

  const refresh = async () => {
    const [projectRes, deviceRes, commandRes] = await Promise.all([queryProjects(), queryDevices(), queryCommands()]);
    projects.value = projectRes.data;
    devices.value = deviceRes.data;
    commands.value = commandRes.data;
    if (!selectedProjectId.value && projects.value[0]) selectedProjectId.value = projects.value[0].id;
    if (!selectedDeviceId.value && devices.value[0]) selectedDeviceId.value = devices.value[0].id;
  };

  watch(selectedProjectId, () => {
    const firstDevice = devices.value.find((device) => device.project_id === selectedProjectId.value);
    selectedDeviceId.value = firstDevice?.id || '';
  });

  const loadCommandDetail = async (record: CommandRecord) => {
    const res = await queryCommandDetail(record.id);
    commandDetail.value = res.data;
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
      await refresh();
      await loadCommandDetail(res.data);
      Message.success('Command sent');
    } finally {
      setLoading(false);
    }
  };

  onMounted(refresh);
</script>

<style lang="less" scoped>
  pre {
    max-height: 260px;
    margin-top: 16px;
    padding: 12px;
    overflow: auto;
    background: var(--color-fill-2);
    border-radius: 4px;
  }
</style>
