<template>
  <div class="page-container dashboard-page">
    <a-grid :cols="24" :col-gap="16" :row-gap="16">
      <a-grid-item v-for="item in stats" :key="item.label" :span="6">
        <a-card :bordered="false">
          <a-statistic :title="item.label" :value="item.value" :value-style="{ color: item.color }" />
        </a-card>
      </a-grid-item>
      <a-grid-item :span="14">
        <a-card title="Command Status" :bordered="false">
          <a-table size="small" row-key="id" :pagination="{ pageSize: 6 }" :data="commands" :columns="commandColumns">
            <template #status="{ record }">
              <a-tag :color="statusColor(record.status)">{{ record.status }}</a-tag>
            </template>
          </a-table>
        </a-card>
      </a-grid-item>
      <a-grid-item :span="10">
        <a-card title="Quick Actions" :bordered="false">
          <a-space direction="vertical" fill>
            <a-button type="primary" @click="$router.push('/projects/index')">Create Project</a-button>
            <a-button @click="$router.push('/devices/index')">Add Device</a-button>
            <a-button @click="$router.push('/commands/index')">Send Command</a-button>
            <a-button @click="$router.push('/simulator/index')">Simulator</a-button>
          </a-space>
        </a-card>
      </a-grid-item>
      <a-grid-item :span="12">
        <a-card title="Webhook Deliveries" :bordered="false">
          <a-table size="small" row-key="id" :pagination="{ pageSize: 6 }" :data="webhooks" :columns="webhookColumns">
            <template #status="{ record }">
              <a-tag :color="statusColor(record.status)">{{ record.status }}</a-tag>
            </template>
          </a-table>
        </a-card>
      </a-grid-item>
      <a-grid-item :span="12">
        <a-card title="Audit Logs" :bordered="false">
          <a-table size="small" row-key="id" :pagination="{ pageSize: 6 }" :data="audits" :columns="auditColumns" />
        </a-card>
      </a-grid-item>
    </a-grid>
  </div>
</template>

<script lang="ts" setup>
  import { computed, onMounted, ref } from 'vue';
  import {
    AuditLogRecord,
    CommandRecord,
    DeviceRecord,
    ProjectRecord,
    WebhookDeliveryRecord,
    queryAuditLogs,
    queryCommands,
    queryDevices,
    queryProjects,
    queryWebhookDeliveries,
  } from '@/api/device-platform';

  defineOptions({ name: 'DashboardWorkplace' });

  const projects = ref<ProjectRecord[]>([]);
  const devices = ref<DeviceRecord[]>([]);
  const commands = ref<CommandRecord[]>([]);
  const webhooks = ref<WebhookDeliveryRecord[]>([]);
  const audits = ref<AuditLogRecord[]>([]);

  const stats = computed(() => [
    { label: 'Projects', value: projects.value.length, color: 'rgb(var(--primary-6))' },
    { label: 'Devices', value: devices.value.length, color: 'rgb(var(--green-6))' },
    { label: 'Commands', value: commands.value.length, color: 'rgb(var(--orange-6))' },
    { label: 'Webhooks', value: webhooks.value.length, color: 'rgb(var(--purple-6))' },
  ]);

  const commandColumns = computed(() => [
    { title: 'ID', dataIndex: 'id', ellipsis: true, tooltip: true },
    { title: 'Type', dataIndex: 'command_type' },
    { title: 'Status', slotName: 'status' },
  ]);

  const webhookColumns = computed(() => [
    { title: 'ID', dataIndex: 'id', ellipsis: true, tooltip: true },
    { title: 'Status', slotName: 'status' },
    { title: 'Attempts', dataIndex: 'attempt_count', width: 90 },
  ]);

  const auditColumns = computed(() => [
    { title: 'Action', dataIndex: 'action' },
    { title: 'Actor', dataIndex: 'actor_id' },
    { title: 'Created', dataIndex: 'created_at', ellipsis: true, tooltip: true },
  ]);

  const statusColor = (status: string) => {
    if (['success', 'delivered', 'online'].includes(status)) return 'green';
    if (['failed', 'dead', 'timeout'].includes(status)) return 'red';
    if (['offline', 'pending'].includes(status)) return 'orange';
    return 'arcoblue';
  };

  const refresh = async () => {
    const [projectRes, deviceRes, commandRes, webhookRes, auditRes] = await Promise.all([
      queryProjects(),
      queryDevices(),
      queryCommands(),
      queryWebhookDeliveries(),
      queryAuditLogs(),
    ]);
    projects.value = projectRes.data;
    devices.value = deviceRes.data;
    commands.value = commandRes.data;
    webhooks.value = webhookRes.data;
    audits.value = auditRes.data;
  };

  onMounted(refresh);
</script>
