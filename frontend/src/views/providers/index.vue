<template>
  <div class="page-container device-platform-page">
    <section class="dp-overview">
      <div class="dp-overview-main">
        <div class="dp-kicker">{{ t('devicePlatform.kicker') }}</div>
        <h1 class="dp-title">{{ t('devicePlatform.providers.title') }}</h1>
        <p class="dp-description">{{ t('devicePlatform.providers.description') }}</p>
      </div>
      <div class="dp-overview-actions">
        <a-button :loading="providersLoading || deviceTypesLoading" @click="refreshAll">
          <template #icon><icon-refresh /></template>
          {{ t('devicePlatform.common.refresh') }}
        </a-button>
      </div>
      <div class="dp-metrics">
        <div class="dp-metric">
          <div class="dp-metric-label">{{ t('devicePlatform.providers.metric.total') }}</div>
          <div class="dp-metric-value">{{ providerPagination.total }}</div>
        </div>
        <div class="dp-metric is-green">
          <div class="dp-metric-label">{{ t('devicePlatform.providers.metric.configuredPage') }}</div>
          <div class="dp-metric-value">{{ configuredProviderCount }}</div>
        </div>
        <div class="dp-metric is-purple">
          <div class="dp-metric-label">{{ t('devicePlatform.providers.metric.deviceTypes') }}</div>
          <div class="dp-metric-value">{{ deviceTypePagination.total }}</div>
        </div>
        <div class="dp-metric is-orange">
          <div class="dp-metric-label">{{ t('devicePlatform.providers.metric.actionsPage') }}</div>
          <div class="dp-metric-value">{{ actionCount }}</div>
        </div>
      </div>
    </section>

    <a-alert type="info">{{ t('devicePlatform.providers.statusNotice') }}</a-alert>

    <Grid :title="t('devicePlatform.providers.registry.title')">
      <GridToolbar @refresh="refreshProviders" />
      <GridTable
        class="dp-table"
        :loading="providersLoading"
        :data="providers"
        :columns="providerColumns"
        :pagination="providerPagination"
        :scroll="{ x: 1160 }"
        @page-change="onProviderPageChange"
        @page-size-change="onProviderPageSizeChange"
      >
        <template #provider="{ record }">
          <div class="dp-cell-primary">{{ record.name }}</div>
          <div class="dp-cell-secondary dp-monospace">{{ record.code }}</div>
        </template>
        <template #transport="{ record }">
          <div class="dp-cell-primary">{{ record.transport_protocol }}</div>
          <div class="dp-cell-secondary dp-monospace">{{ record.adapter }}</div>
        </template>
        <template #profiles="{ record }">
          <div class="dp-tag-list">
            <a-tag v-for="profile in record.profiles" :key="profile">{{ profile }}</a-tag>
          </div>
        </template>
        <template #integrationStatus="{ record }">
          <a-tag :color="providerStatusColor(record.integration_status)">
            {{ record.integration_status }}
          </a-tag>
        </template>
      </GridTable>
    </Grid>

    <Grid :title="t('devicePlatform.providers.deviceTypes.title')">
      <GridToolbar @refresh="refreshDeviceTypes" />
      <GridTable
        class="dp-table"
        :loading="deviceTypesLoading"
        :data="deviceTypes"
        :columns="deviceTypeColumns"
        :pagination="deviceTypePagination"
        :scroll="{ x: 760 }"
        @page-change="onDeviceTypePageChange"
        @page-size-change="onDeviceTypePageSizeChange"
      >
        <template #deviceType="{ record }">
          <div class="dp-cell-primary">{{ record.name }}</div>
          <div class="dp-cell-secondary dp-monospace">{{ record.code }}</div>
        </template>
        <template #revision="{ record }">
          <span class="dp-monospace">r{{ record.revision }}</span>
        </template>
        <template #actionIdentifiers="{ record }">
          <div v-if="record.actions.length" class="dp-tag-list">
            <a-tag v-for="action in record.actions" :key="action.identifier" color="arcoblue">
              {{ action.identifier }}
            </a-tag>
          </div>
          <span v-else class="dp-muted">{{ t('devicePlatform.common.empty') }}</span>
        </template>
        <template #action="{ record }">
          <a-button type="text" size="small" @click="openDeviceTypeDetail(record)">
            {{ t('devicePlatform.providers.action.detail') }}
          </a-button>
        </template>
      </GridTable>
    </Grid>

    <a-drawer
      v-model:visible="deviceTypeDetailVisible"
      :title="t('devicePlatform.providers.drawer.title')"
      :footer="false"
      width="min(100vw, 720px)"
    >
      <template v-if="selectedDeviceType">
        <a-descriptions :column="1" size="small" bordered>
          <a-descriptions-item :label="t('devicePlatform.providers.columns.deviceType')">
            <div class="dp-cell-primary">{{ selectedDeviceType.name }}</div>
            <div class="dp-cell-secondary dp-monospace">{{ selectedDeviceType.code }}</div>
          </a-descriptions-item>
          <a-descriptions-item :label="t('devicePlatform.providers.columns.revision')">
            r{{ selectedDeviceType.revision }}
          </a-descriptions-item>
        </a-descriptions>

        <h3 class="dp-panel-title provider-actions-title">{{ t('devicePlatform.providers.detail.actions') }}</h3>
        <a-empty v-if="!selectedDeviceType.actions.length" />
        <div v-else class="provider-action-list">
          <section v-for="action in selectedDeviceType.actions" :key="action.identifier" class="provider-action">
            <div class="provider-action-heading">
              <span class="dp-cell-primary dp-monospace">{{ action.identifier }}</span>
              <a-tag :color="riskColor(action.risk_level)">{{ action.risk_level }}</a-tag>
            </div>
            <a-descriptions :column="1" size="small" bordered>
              <a-descriptions-item :label="t('devicePlatform.providers.detail.deliveryPolicy')">
                <span class="dp-monospace">{{ action.delivery_policy }}</span>
              </a-descriptions-item>
              <a-descriptions-item :label="t('devicePlatform.providers.detail.timeouts')">
                <div class="provider-timeouts">
                  <span>{{ t('devicePlatform.providers.detail.dispatchDeadline') }}: {{ action.dispatch_deadline_ms }} ms</span>
                  <span
                    >{{ t('devicePlatform.providers.detail.providerTimeout') }}:
                    {{ action.provider_request_timeout_ms }} ms</span
                  >
                  <span
                    >{{ t('devicePlatform.providers.detail.resultTimeout') }}:
                    {{ action.result_observation_timeout_ms }} ms</span
                  >
                </div>
              </a-descriptions-item>
              <a-descriptions-item :label="t('devicePlatform.providers.detail.retry')">
                {{ action.retry_allowed ? t('devicePlatform.common.yes') : t('devicePlatform.common.no') }}
              </a-descriptions-item>
              <a-descriptions-item :label="t('devicePlatform.providers.detail.policyOverride')">
                {{ action.delivery_policy_override_allowed ? t('devicePlatform.common.yes') : t('devicePlatform.common.no') }}
              </a-descriptions-item>
            </a-descriptions>
            <h4 class="provider-schema-title">{{ t('devicePlatform.providers.detail.payloadSchema') }}</h4>
            <pre class="dp-json">{{ JSON.stringify(action.payload_schema, null, 2) }}</pre>
          </section>
        </div>
      </template>
    </a-drawer>
  </div>
</template>

<script lang="ts" setup>
  import { computed, onMounted, reactive, ref } from 'vue';
  import { useI18n } from 'vue-i18n';
  import { CloudProviderRecord, DeviceTypeRecord, queryCloudProviders, queryDeviceTypes } from '@/api/device-platform';
  import '../device-platform/workspace.less';

  defineOptions({ name: 'ProvidersIndex' });

  const { t } = useI18n();
  const providers = ref<CloudProviderRecord[]>([]);
  const deviceTypes = ref<DeviceTypeRecord[]>([]);
  const selectedDeviceType = ref<DeviceTypeRecord>();
  const providersLoading = ref(false);
  const deviceTypesLoading = ref(false);
  const deviceTypeDetailVisible = ref(false);
  const providerPagination = reactive({
    current: 1,
    pageSize: 10,
    total: 0,
    showTotal: true,
    showPageSize: true,
  });
  const deviceTypePagination = reactive({
    current: 1,
    pageSize: 10,
    total: 0,
    showTotal: true,
    showPageSize: true,
  });

  const configuredProviderCount = computed(
    () => providers.value.filter((provider) => provider.integration_status !== 'unconfigured').length
  );
  const actionCount = computed(() => deviceTypes.value.reduce((total, item) => total + item.actions.length, 0));
  const providerColumns = computed(() => [
    { title: t('devicePlatform.providers.columns.provider'), slotName: 'provider', width: 230 },
    { title: t('devicePlatform.common.access'), dataIndex: 'access_type', width: 160 },
    { title: t('devicePlatform.providers.columns.transport'), slotName: 'transport', width: 250 },
    { title: t('devicePlatform.providers.columns.profiles'), slotName: 'profiles', width: 320 },
    { title: t('devicePlatform.providers.columns.integrationStatus'), slotName: 'integrationStatus', width: 250 },
  ]);
  const deviceTypeColumns = computed(() => [
    { title: t('devicePlatform.providers.columns.deviceType'), slotName: 'deviceType', width: 260 },
    { title: t('devicePlatform.providers.columns.revision'), slotName: 'revision', width: 110 },
    { title: t('devicePlatform.providers.columns.actions'), slotName: 'actionIdentifiers', width: 330 },
    { title: t('devicePlatform.common.actions'), slotName: 'action', width: 100 },
  ]);

  const providerStatusColor = (status: CloudProviderRecord['integration_status']) => {
    if (status === 'verified') return 'green';
    if (status === 'configured_unverified') return 'orange';
    return 'gray';
  };

  const riskColor = (risk: string) => {
    if (risk === 'high') return 'red';
    if (risk === 'medium') return 'orange';
    return 'blue';
  };

  const refreshProviders = async () => {
    providersLoading.value = true;
    try {
      const res = await queryCloudProviders({
        page: providerPagination.current,
        page_size: providerPagination.pageSize,
      });
      providers.value = res.data;
      providerPagination.total = res.meta?.total ?? res.data.length;
    } finally {
      providersLoading.value = false;
    }
  };

  const refreshDeviceTypes = async () => {
    deviceTypesLoading.value = true;
    try {
      const res = await queryDeviceTypes({
        page: deviceTypePagination.current,
        page_size: deviceTypePagination.pageSize,
      });
      deviceTypes.value = res.data;
      deviceTypePagination.total = res.meta?.total ?? res.data.length;
    } finally {
      deviceTypesLoading.value = false;
    }
  };

  const refreshAll = () => Promise.all([refreshProviders(), refreshDeviceTypes()]);

  const onProviderPageChange = (page: number) => {
    providerPagination.current = page;
    refreshProviders();
  };

  const onProviderPageSizeChange = (pageSize: number) => {
    providerPagination.pageSize = pageSize;
    providerPagination.current = 1;
    refreshProviders();
  };

  const onDeviceTypePageChange = (page: number) => {
    deviceTypePagination.current = page;
    refreshDeviceTypes();
  };

  const onDeviceTypePageSizeChange = (pageSize: number) => {
    deviceTypePagination.pageSize = pageSize;
    deviceTypePagination.current = 1;
    refreshDeviceTypes();
  };

  const openDeviceTypeDetail = (record: DeviceTypeRecord) => {
    selectedDeviceType.value = record;
    deviceTypeDetailVisible.value = true;
  };

  onMounted(refreshAll);
</script>

<style lang="less" scoped>
  .provider-actions-title {
    margin-top: 20px;
  }

  .provider-action {
    padding: 16px 0 20px;
    border-bottom: 1px solid var(--color-border-2);
  }

  .provider-action:first-child {
    padding-top: 0;
  }

  .provider-action:last-child {
    border-bottom: 0;
  }

  .provider-action-heading {
    display: flex;
    gap: 12px;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 12px;
  }

  .provider-timeouts {
    display: grid;
    gap: 4px;
  }

  .provider-schema-title {
    margin: 14px 0 0;
    color: var(--color-text-2);
    font-weight: 500;
    font-size: 13px;
  }
</style>
