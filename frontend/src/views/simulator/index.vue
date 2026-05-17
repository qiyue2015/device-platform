<template>
  <div class="page-container device-platform-page">
    <section class="dp-overview">
      <div class="dp-overview-main">
        <div class="dp-kicker">{{ t('devicePlatform.kicker') }}</div>
        <h1 class="dp-title">{{ t('devicePlatform.simulator.title') }}</h1>
        <p class="dp-description">{{ t('devicePlatform.simulator.description') }}</p>
      </div>
      <div class="dp-overview-actions">
        <a-button @click="refresh">
          <template #icon><icon-refresh /></template>
          {{ t('devicePlatform.common.refresh') }}
        </a-button>
      </div>
      <div class="dp-metrics">
        <div class="dp-metric">
          <div class="dp-metric-label">{{ t('devicePlatform.simulator.metric.mode') }}</div>
          <div class="dp-metric-value">{{ simulator?.mode || '-' }}</div>
        </div>
        <div class="dp-metric" :class="simulator?.heartbeat_active ? 'is-green' : 'is-orange'">
          <div class="dp-metric-label">{{ t('devicePlatform.simulator.metric.heartbeat') }}</div>
          <div class="dp-metric-value">{{ simulatorConnectionMeta.label }}</div>
        </div>
        <div class="dp-metric is-purple">
          <div class="dp-metric-label">{{ t('devicePlatform.simulator.metric.delay') }}</div>
          <div class="dp-metric-value">{{ simulator?.delay_ms ?? simulatorDelay }}ms</div>
        </div>
        <div class="dp-metric is-orange">
          <div class="dp-metric-label">{{ t('devicePlatform.simulator.metric.updated') }}</div>
          <div class="dp-metric-value simulator-updated">{{ simulator?.updated_at || '-' }}</div>
        </div>
      </div>
    </section>

    <Grid :title="t('devicePlatform.simulator.surface.title')">
      <div class="dp-split-panel">
        <section class="dp-panel">
          <h2 class="dp-panel-title">{{ t('devicePlatform.simulator.control.mode') }}</h2>
          <a-radio-group v-model="simulatorMode" type="button" data-testid="simulator-mode">
            <a-radio v-for="mode in simulatorModes" :key="mode" :value="mode">{{ mode }}</a-radio>
          </a-radio-group>
          <h2 class="dp-panel-title simulator-control-title">{{ t('devicePlatform.simulator.control.delay') }}</h2>
          <a-input-number v-model="simulatorDelay" :min="0" :step="100" hide-button>
            <template #append>ms</template>
          </a-input-number>
          <a-button
            type="primary"
            class="simulator-apply"
            data-testid="apply-simulator-mode"
            :loading="loading"
            @click="handleSimulatorUpdate"
          >
            <template #icon><icon-sync /></template>
            {{ t('devicePlatform.simulator.action.apply') }}
          </a-button>
        </section>
        <section class="dp-panel">
          <h2 class="dp-panel-title">{{ t('devicePlatform.simulator.statusPanel.title') }}</h2>
          <a-skeleton v-if="!simulator" animation />
          <a-descriptions v-else :column="1" bordered>
            <a-descriptions-item :label="t('devicePlatform.simulator.metric.mode')">{{ simulator.mode }}</a-descriptions-item>
            <a-descriptions-item :label="t('devicePlatform.simulator.metric.heartbeat')">
              <a-tag :color="simulatorConnectionMeta.color">{{ simulatorConnectionMeta.label }}</a-tag>
            </a-descriptions-item>
            <a-descriptions-item :label="t('devicePlatform.simulator.metric.delay')"
              >{{ simulator.delay_ms }}ms</a-descriptions-item
            >
            <a-descriptions-item :label="t('devicePlatform.common.updatedAt')">{{ simulator.updated_at }}</a-descriptions-item>
          </a-descriptions>
        </section>
      </div>
    </Grid>
  </div>
</template>

<script lang="ts" setup>
  import { computed, onMounted, ref } from 'vue';
  import { useI18n } from 'vue-i18n';
  import { Message } from '@arco-design/web-vue';
  import useLoading from '@/hooks/loading';
  import { SimulatorState, getSimulator, updateSimulator } from '@/api/device-platform';
  import { getBusinessStatusMeta } from '@/utils/device-platform-status';
  import '../device-platform/workspace.less';

  defineOptions({ name: 'SimulatorIndex' });

  const { t } = useI18n();
  const { loading, setLoading } = useLoading(false);
  const simulator = ref<SimulatorState>();
  const simulatorMode = ref('normal');
  const simulatorDelay = ref(800);
  const simulatorModes = ['normal', 'delay', 'offline', 'timeout_then_ack', 'duplicate_ack', 'fail'];
  const simulatorConnectionMeta = computed(() =>
    getBusinessStatusMeta('connection', simulator.value?.heartbeat_active ? 'online' : 'offline')
  );

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
      Message.success(t('devicePlatform.simulator.message.updated'));
    } finally {
      setLoading(false);
    }
  };

  onMounted(refresh);
</script>

<style lang="less" scoped>
  .simulator-control-title {
    margin-top: 20px;
  }

  .simulator-apply {
    width: 100%;
    margin-top: 20px;
  }

  .simulator-updated {
    overflow: hidden;
    font-size: 14px;
    white-space: nowrap;
    text-overflow: ellipsis;
  }
</style>
