<template>
  <div class="page-container device-platform-page">
    <a-row :gutter="16">
      <a-col :span="8">
        <a-card title="Projects" :bordered="false">
          <a-space direction="vertical" fill>
            <a-form :model="projectForm" layout="vertical">
              <a-form-item label="Name">
                <a-input v-model="projectForm.name" data-testid="project-name" />
              </a-form-item>
              <a-form-item label="Webhook URL">
                <a-input v-model="projectForm.webhook_url" data-testid="project-webhook-url" />
              </a-form-item>
              <a-form-item label="IP whitelist">
                <a-input v-model="projectWhitelist" data-testid="project-ip-whitelist" placeholder="127.0.0.1, 10.0.0.0/8" />
              </a-form-item>
              <a-button type="primary" long data-testid="create-project" :loading="loading" @click="handleCreateProject">
                <template #icon><icon-plus /></template>
                Create Project
              </a-button>
            </a-form>
            <a-list size="small" :data="projects">
              <template #item="{ item }">
                <a-list-item class="selectable" :data-testid="`project-row-${item.id}`" @click="selectedProjectId = item.id">
                  <a-list-item-meta :title="item.name" :description="item.id" />
                  <template #actions>
                    <a-tag v-if="selectedProjectId === item.id" color="arcoblue">active</a-tag>
                  </template>
                </a-list-item>
              </template>
            </a-list>
          </a-space>
        </a-card>
      </a-col>
      <a-col :span="8">
        <a-card title="Devices" :bordered="false">
          <a-space direction="vertical" fill>
            <a-form :model="deviceForm" layout="vertical">
              <a-form-item label="Name">
                <a-input v-model="deviceForm.name" data-testid="device-name" />
              </a-form-item>
              <a-form-item label="Access type">
                <a-select v-model="deviceForm.access_type" data-testid="device-access-type">
                  <a-option value="simulator_gateway">simulator_gateway</a-option>
                  <a-option value="cloud_api">cloud_api</a-option>
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
            <a-table
              size="small"
              row-key="id"
              :pagination="false"
              :data="devices"
              :columns="deviceColumns"
              @row-click="(record: DeviceRecord) => (selectedDeviceId = record.id)"
            >
              <template #status="{ record }">
                <a-tag :color="record.connection_status === 'online' ? 'green' : 'orange'">
                  {{ record.connection_status }}
                </a-tag>
              </template>
            </a-table>
          </a-space>
        </a-card>
      </a-col>
      <a-col :span="8">
        <a-card title="Simulator" :bordered="false">
          <a-space direction="vertical" fill>
            <a-radio-group v-model="simulatorMode" type="button" data-testid="simulator-mode">
              <a-radio v-for="mode in simulatorModes" :key="mode" :value="mode">{{ mode }}</a-radio>
            </a-radio-group>
            <a-input-number v-model="simulatorDelay" :min="0" :step="100" hide-button>
              <template #append>ms</template>
            </a-input-number>
            <a-button type="primary" long data-testid="apply-simulator-mode" :loading="loading" @click="handleSimulatorUpdate">
              <template #icon><icon-sync /></template>
              Apply Mode
            </a-button>
            <a-descriptions v-if="simulator" :column="1" size="small" bordered>
              <a-descriptions-item label="Mode">{{ simulator.mode }}</a-descriptions-item>
              <a-descriptions-item label="Heartbeat">
                {{ simulator.heartbeat_active ? 'active' : 'stopped' }}
              </a-descriptions-item>
              <a-descriptions-item label="Updated">{{ simulator.updated_at }}</a-descriptions-item>
            </a-descriptions>
          </a-space>
        </a-card>
      </a-col>
    </a-row>

    <a-row :gutter="16" class="mt-4">
      <a-col :span="12">
        <a-card title="Command Lab" :bordered="false">
          <a-space direction="vertical" fill>
            <a-space>
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
              size="small"
              row-key="id"
              :pagination="{ pageSize: 6 }"
              :data="commands"
              :columns="commandColumns"
              @row-click="loadCommandDetail"
            >
              <template #commandStatus="{ record }">
                <a-tag :color="statusColor(record.status)">{{ record.status }}</a-tag>
              </template>
            </a-table>
          </a-space>
        </a-card>
      </a-col>
      <a-col :span="12">
        <a-card title="Command Detail" :bordered="false">
          <a-empty v-if="!commandDetail" />
          <a-space v-else direction="vertical" fill>
            <a-descriptions :column="2" size="small" bordered>
              <a-descriptions-item label="Command">{{ commandDetail.command.id }}</a-descriptions-item>
              <a-descriptions-item label="Status">{{ commandDetail.command.status }}</a-descriptions-item>
              <a-descriptions-item label="Policy">{{ commandDetail.command.delivery_policy }}</a-descriptions-item>
              <a-descriptions-item label="Corrected">{{ commandDetail.command.corrected ? 'yes' : 'no' }}</a-descriptions-item>
            </a-descriptions>
            <a-typography-title :heading="6">Attempts</a-typography-title>
            <pre>{{ JSON.stringify(commandDetail.attempts, null, 2) }}</pre>
            <a-typography-title :heading="6">Events</a-typography-title>
            <pre>{{ JSON.stringify(commandDetail.events, null, 2) }}</pre>
          </a-space>
        </a-card>
      </a-col>
    </a-row>

    <a-row :gutter="16" class="mt-4">
      <a-col :span="14">
        <a-card title="Webhook Deliveries" :bordered="false">
          <a-table size="small" row-key="id" :pagination="{ pageSize: 6 }" :data="webhooks" :columns="webhookColumns">
            <template #webhookStatus="{ record }">
              <a-tag :color="statusColor(record.status)">{{ record.status }}</a-tag>
            </template>
            <template #webhookActions="{ record }">
              <a-button size="mini" data-testid="resend-webhook" @click="handleResendWebhook(record.id)">Resend</a-button>
            </template>
          </a-table>
        </a-card>
      </a-col>
      <a-col :span="10">
        <a-card title="Audit Logs" :bordered="false">
          <a-table size="small" row-key="id" :pagination="{ pageSize: 6 }" :data="audits" :columns="auditColumns" />
        </a-card>
      </a-col>
    </a-row>
  </div>
</template>

<script lang="ts" setup>
  import { computed, onMounted, reactive, ref } from 'vue';
  import { Message } from '@arco-design/web-vue';
  import useLoading from '@/hooks/loading';
  import {
    AuditLogRecord,
    CommandDetail,
    CommandRecord,
    DeviceRecord,
    ProjectRecord,
    SimulatorState,
    WebhookDeliveryRecord,
    createCommand,
    createDevice,
    createProject,
    getSimulator,
    queryAuditLogs,
    queryCommandDetail,
    queryCommands,
    queryDevices,
    queryProjects,
    queryWebhookDeliveries,
    resendWebhookDelivery,
    updateSimulator,
  } from '@/api/device-platform';

  defineOptions({ name: 'DevicePlatformConsole' });

  const { loading, setLoading } = useLoading(false);
  const projects = ref<ProjectRecord[]>([]);
  const devices = ref<DeviceRecord[]>([]);
  const commands = ref<CommandRecord[]>([]);
  const webhooks = ref<WebhookDeliveryRecord[]>([]);
  const audits = ref<AuditLogRecord[]>([]);
  const simulator = ref<SimulatorState>();
  const selectedProjectId = ref('');
  const selectedDeviceId = ref('');
  const commandDetail = ref<CommandDetail>();
  const projectWhitelist = ref('');
  const simulatorMode = ref('normal');
  const simulatorDelay = ref(800);

  const projectForm = reactive({
    name: 'Smoke Test Project',
    webhook_url: 'https://example.com/device-webhook',
  });
  const deviceForm = reactive({
    name: 'Smoke Test Lock',
    access_type: 'simulator_gateway',
    provider_device_id: '',
  });
  const commandForm = reactive({
    command_type: 'query_status',
    idempotency_key: '',
  });

  const simulatorModes = ['normal', 'delay', 'offline', 'timeout_then_ack', 'duplicate_ack', 'fail'];

  const deviceColumns = computed(() => [
    { title: 'Name', dataIndex: 'name' },
    { title: 'Access', dataIndex: 'access_type' },
    { title: 'Provider', dataIndex: 'provider_code' },
    { title: 'Provider Device', dataIndex: 'provider_device_id' },
    { title: 'Adapter', dataIndex: 'adapter' },
    { title: 'Status', slotName: 'status' },
  ]);

  const commandColumns = computed(() => [
    { title: 'ID', dataIndex: 'id', ellipsis: true, tooltip: true },
    { title: 'Device', dataIndex: 'device_id', ellipsis: true, tooltip: true },
    { title: 'Type', dataIndex: 'command_type' },
    { title: 'Policy', dataIndex: 'delivery_policy' },
    { title: 'Status', slotName: 'commandStatus' },
  ]);

  const webhookColumns = computed(() => [
    { title: 'ID', dataIndex: 'id', ellipsis: true, tooltip: true },
    { title: 'Event', dataIndex: 'event_id', ellipsis: true, tooltip: true },
    { title: 'Status', slotName: 'webhookStatus' },
    { title: 'Attempts', dataIndex: 'attempt_count', width: 90 },
    { title: 'Actions', slotName: 'webhookActions', width: 100 },
  ]);

  const auditColumns = computed(() => [
    { title: 'Action', dataIndex: 'action' },
    { title: 'Actor', dataIndex: 'actor_id' },
    { title: 'IP', dataIndex: 'ip' },
    { title: 'Created', dataIndex: 'created_at', ellipsis: true, tooltip: true },
  ]);

  const statusColor = (status: string) => {
    if (['success', 'delivered', 'online'].includes(status)) return 'green';
    if (['failed', 'dead', 'timeout'].includes(status)) return 'red';
    if (['offline', 'pending'].includes(status)) return 'orange';
    return 'arcoblue';
  };

  const refreshAll = async () => {
    const [projectRes, deviceRes, commandRes, webhookRes, auditRes, simulatorRes] = await Promise.all([
      queryProjects(),
      queryDevices(),
      queryCommands(),
      queryWebhookDeliveries(),
      queryAuditLogs(),
      getSimulator(),
    ]);
    projects.value = projectRes.data;
    devices.value = deviceRes.data;
    commands.value = commandRes.data;
    webhooks.value = webhookRes.data;
    audits.value = auditRes.data;
    simulator.value = simulatorRes.data;
    simulatorMode.value = simulatorRes.data.mode;
    simulatorDelay.value = simulatorRes.data.delay_ms;
    if (!selectedProjectId.value && projects.value[0]) selectedProjectId.value = projects.value[0].id;
    if (!selectedDeviceId.value && devices.value[0]) selectedDeviceId.value = devices.value[0].id;
  };

  const runAction = async (action: () => Promise<void>, message: string) => {
    setLoading(true);
    try {
      await action();
      await refreshAll();
      Message.success(message);
    } finally {
      setLoading(false);
    }
  };

  const handleCreateProject = () =>
    runAction(async () => {
      const whitelist = projectWhitelist.value
        .split(',')
        .map((item) => item.trim())
        .filter(Boolean);
      const res = await createProject({ ...projectForm, ip_whitelist: whitelist });
      selectedProjectId.value = res.data.id;
    }, 'Project created');

  const handleCreateDevice = () =>
    runAction(async () => {
      const accessType = deviceForm.access_type;
      const res = await createDevice({
        project_id: selectedProjectId.value,
        name: deviceForm.name,
        device_type: 'smart_lock',
        access_type: accessType,
        provider_device_id: deviceForm.provider_device_id,
        provider_code: accessType === 'cloud_api' ? 'wwtiot' : 'simulator',
        transport_protocol: 'http',
        adapter: accessType === 'cloud_api' ? 'wwtiot_cloud_api' : 'simulator_http',
      });
      selectedDeviceId.value = res.data.id;
    }, 'Device created');

  const handleSimulatorUpdate = () =>
    runAction(async () => {
      await updateSimulator({ mode: simulatorMode.value, delay_ms: simulatorDelay.value });
    }, 'Simulator updated');

  const loadCommandDetail = async (record: CommandRecord) => {
    const res = await queryCommandDetail(record.id);
    commandDetail.value = res.data;
  };

  const handleCreateCommand = () =>
    runAction(async () => {
      const res = await createCommand({
        project_id: selectedProjectId.value,
        device_id: selectedDeviceId.value,
        command_type: commandForm.command_type,
        idempotency_key: commandForm.idempotency_key || `ui-${Date.now()}`,
      });
      await loadCommandDetail(res.data);
    }, 'Command sent');

  const handleResendWebhook = (id: string) =>
    runAction(async () => {
      await resendWebhookDelivery(id);
    }, 'Webhook resend queued');

  onMounted(refreshAll);
</script>

<style lang="less" scoped>
  .device-platform-page {
    min-height: 100%;
  }

  .mt-4 {
    margin-top: 16px;
  }

  .selectable {
    cursor: pointer;
  }

  pre {
    max-height: 180px;
    padding: 12px;
    overflow: auto;
    background: var(--color-fill-2);
    border-radius: 4px;
  }
</style>
