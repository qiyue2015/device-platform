<template>
  <div class="page-container device-platform-page">
    <section class="dp-overview">
      <div class="dp-overview-main">
        <div class="dp-kicker">{{ t('devicePlatform.kicker') }}</div>
        <h1 class="dp-title">{{ t('devicePlatform.projects.title') }}</h1>
        <p class="dp-description">{{ t('devicePlatform.projects.description') }}</p>
      </div>
      <div class="dp-overview-actions">
        <a-space>
          <a-button @click="refresh">
            <template #icon><icon-refresh /></template>
            {{ t('devicePlatform.common.refresh') }}
          </a-button>
        </a-space>
      </div>
      <div class="dp-metrics">
        <div class="dp-metric">
          <div class="dp-metric-label">{{ t('devicePlatform.projects.metric.total') }}</div>
          <div class="dp-metric-value">{{ projects.length }}</div>
        </div>
        <div class="dp-metric is-green">
          <div class="dp-metric-label">{{ t('devicePlatform.projects.metric.webhook') }}</div>
          <div class="dp-metric-value">{{ webhookCount }}</div>
        </div>
        <div class="dp-metric is-orange">
          <div class="dp-metric-label">{{ t('devicePlatform.projects.metric.whitelist') }}</div>
          <div class="dp-metric-value">{{ whitelistCount }}</div>
        </div>
        <div class="dp-metric is-purple">
          <div class="dp-metric-label">{{ t('devicePlatform.projects.metric.apiKey') }}</div>
          <div class="dp-metric-value">{{ apiKeyCount }}</div>
        </div>
      </div>
    </section>

    <Grid :title="t('devicePlatform.projects.table.title')">
      <GridToolbar @refresh="refresh">
        <template #prepend>
          <a-form :model="projectForm" layout="inline" class="dp-inline-form">
            <a-form-item :label="t('devicePlatform.projects.form.name')">
              <a-input
                v-model="projectForm.name"
                class="dp-toolbar-control"
                data-testid="project-name"
                :placeholder="t('devicePlatform.projects.form.name.placeholder')"
              />
            </a-form-item>
            <a-form-item :label="t('devicePlatform.projects.form.webhook')">
              <a-input
                v-model="projectForm.webhook_url"
                class="dp-toolbar-control is-wide"
                data-testid="project-webhook-url"
                :placeholder="t('devicePlatform.projects.form.webhook.placeholder')"
              />
            </a-form-item>
            <a-form-item :label="t('devicePlatform.projects.form.whitelist')">
              <a-input
                v-model="projectWhitelist"
                class="dp-toolbar-control is-wide"
                data-testid="project-ip-whitelist"
                :placeholder="t('devicePlatform.projects.form.whitelist.placeholder')"
              />
            </a-form-item>
            <a-form-item class="dp-inline-action">
              <a-button type="primary" data-testid="create-project" :loading="loading" @click="handleCreateProject">
                <template #icon><icon-plus /></template>
                {{ t('devicePlatform.projects.action.create') }}
              </a-button>
            </a-form-item>
          </a-form>
        </template>
      </GridToolbar>
      <GridTable
        class="dp-table"
        :loading="loading"
        :data="projects"
        :columns="columns"
        :pagination="{ pageSize: 10 }"
        :scroll="{ x: 1170 }"
      >
        <template #project="{ record }">
          <div class="dp-cell-primary">{{ record.name }}</div>
          <div class="dp-cell-secondary dp-monospace">{{ record.id }}</div>
        </template>
        <template #apiKey="{ record }">
          <a-tag :color="record.api_key ? 'green' : 'gray'">
            {{ record.api_key ? t('devicePlatform.projects.apiKey.issued') : t('devicePlatform.common.notConfigured') }}
          </a-tag>
        </template>
        <template #webhook="{ record }">
          <a-typography-text v-if="record.webhook_url" class="dp-monospace" copyable ellipsis>
            {{ record.webhook_url }}
          </a-typography-text>
          <span v-else class="dp-muted">{{ t('devicePlatform.common.notConfigured') }}</span>
        </template>
        <template #whitelist="{ record }">
          <div v-if="record.ip_whitelist?.length" class="dp-tag-list">
            <a-tag v-for="ip in record.ip_whitelist" :key="ip" color="arcoblue">{{ ip }}</a-tag>
          </div>
          <span v-else class="dp-muted">{{ t('devicePlatform.common.notConfigured') }}</span>
        </template>
      </GridTable>
    </Grid>
  </div>
</template>

<script lang="ts" setup>
  import { computed, onMounted, reactive, ref } from 'vue';
  import { useI18n } from 'vue-i18n';
  import { Message } from '@arco-design/web-vue';
  import useLoading from '@/hooks/loading';
  import { ProjectRecord, createProject, queryProjects } from '@/api/device-platform';
  import '../device-platform/workspace.less';

  defineOptions({ name: 'ProjectsIndex' });

  const { t } = useI18n();
  const { loading, setLoading } = useLoading(false);
  const projects = ref<ProjectRecord[]>([]);
  const projectWhitelist = ref('');
  const projectForm = reactive({
    name: '',
    webhook_url: '',
  });

  const webhookCount = computed(() => projects.value.filter((project) => !!project.webhook_url).length);
  const whitelistCount = computed(() => projects.value.filter((project) => project.ip_whitelist?.length).length);
  const apiKeyCount = computed(() => projects.value.filter((project) => !!project.api_key).length);
  const columns = computed(() => [
    { title: t('devicePlatform.projects.columns.name'), slotName: 'project', width: 260 },
    { title: t('devicePlatform.projects.columns.apiKey'), slotName: 'apiKey', width: 180 },
    { title: t('devicePlatform.projects.columns.webhook'), slotName: 'webhook', width: 320 },
    { title: t('devicePlatform.projects.columns.whitelist'), slotName: 'whitelist', width: 220 },
    { title: t('devicePlatform.common.createdAt'), dataIndex: 'created_at', ellipsis: true, tooltip: true, width: 190 },
  ]);

  const refresh = async () => {
    setLoading(true);
    try {
      const res = await queryProjects();
      projects.value = res.data;
    } finally {
      setLoading(false);
    }
  };

  const handleCreateProject = async () => {
    if (!projectForm.name.trim()) {
      Message.warning(t('devicePlatform.projects.message.nameRequired'));
      return;
    }
    setLoading(true);
    try {
      const whitelist = projectWhitelist.value
        .split(',')
        .map((item) => item.trim())
        .filter(Boolean);
      await createProject({ ...projectForm, name: projectForm.name.trim(), ip_whitelist: whitelist });
      projectForm.name = '';
      projectForm.webhook_url = '';
      projectWhitelist.value = '';
      await refresh();
      Message.success(t('devicePlatform.projects.message.created'));
    } finally {
      setLoading(false);
    }
  };

  onMounted(refresh);
</script>
