<template>
  <div class="page-container">
    <a-card :title="$t('menu.simulator')" :bordered="false">
      <a-space direction="vertical" fill>
        <a-radio-group v-model="simulatorMode" type="button" data-testid="simulator-mode">
          <a-radio v-for="mode in simulatorModes" :key="mode" :value="mode">{{ mode }}</a-radio>
        </a-radio-group>
        <a-input-number v-model="simulatorDelay" :min="0" :step="100" hide-button>
          <template #append>ms</template>
        </a-input-number>
        <a-button type="primary" data-testid="apply-simulator-mode" :loading="loading" @click="handleSimulatorUpdate">
          <template #icon><icon-sync /></template>
          Apply Mode
        </a-button>
        <a-descriptions v-if="simulator" :column="1" bordered>
          <a-descriptions-item label="Mode">{{ simulator.mode }}</a-descriptions-item>
          <a-descriptions-item label="Heartbeat">
            {{ simulator.heartbeat_active ? 'active' : 'stopped' }}
          </a-descriptions-item>
          <a-descriptions-item label="Updated">{{ simulator.updated_at }}</a-descriptions-item>
        </a-descriptions>
      </a-space>
    </a-card>
  </div>
</template>

<script lang="ts" setup>
  import { onMounted, ref } from 'vue';
  import { Message } from '@arco-design/web-vue';
  import useLoading from '@/hooks/loading';
  import { SimulatorState, getSimulator, updateSimulator } from '@/api/device-platform';

  defineOptions({ name: 'SimulatorIndex' });

  const { loading, setLoading } = useLoading(false);
  const simulator = ref<SimulatorState>();
  const simulatorMode = ref('normal');
  const simulatorDelay = ref(800);
  const simulatorModes = ['normal', 'delay', 'offline', 'timeout_then_ack', 'duplicate_ack', 'fail'];

  const refresh = async () => {
    const res = await getSimulator();
    simulator.value = res.data;
    simulatorMode.value = res.data.mode;
    simulatorDelay.value = res.data.delay_ms;
  };

  const handleSimulatorUpdate = async () => {
    setLoading(true);
    try {
      await updateSimulator({ mode: simulatorMode.value, delay_ms: simulatorDelay.value });
      await refresh();
      Message.success('Simulator updated');
    } finally {
      setLoading(false);
    }
  };

  onMounted(refresh);
</script>
