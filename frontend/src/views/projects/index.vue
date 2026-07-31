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
          <div class="dp-metric-value">{{ pagination.total }}</div>
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
          <div class="dp-metric-label">{{ t('devicePlatform.projects.metric.webhookDisabled') }}</div>
          <div class="dp-metric-value">{{ webhookDisabledCount }}</div>
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
        :pagination="pagination"
        :scroll="{ x: 1170 }"
        @page-change="onPageChange"
        @page-size-change="onPageSizeChange"
      >
        <template #project="{ record }">
          <div class="dp-cell-primary">{{ record.name }}</div>
          <div class="dp-cell-secondary dp-monospace">{{ record.id }}</div>
        </template>
        <template #webhook="{ record }">
          <div class="dp-cell-primary">
            <a-tag :color="record.webhook_configured ? 'green' : 'gray'">
              {{
                record.webhook_configured
                  ? t('devicePlatform.projects.webhook.configured')
                  : t('devicePlatform.common.notConfigured')
              }}
            </a-tag>
          </div>
          <a-typography-text v-if="record.webhook_url" class="dp-cell-secondary dp-monospace" copyable ellipsis>
            {{ record.webhook_url }}
          </a-typography-text>
        </template>
        <template #whitelist="{ record }">
          <div v-if="record.ip_whitelist?.length" class="dp-tag-list">
            <a-tag v-for="ip in record.ip_whitelist" :key="ip" color="arcoblue">{{ ip }}</a-tag>
          </div>
          <span v-else class="dp-muted">{{ t('devicePlatform.common.notConfigured') }}</span>
        </template>
        <template #action="{ record }">
          <a-space>
            <a-button type="text" size="small" @click="openEditModal(record)">
              {{ t('devicePlatform.projects.action.edit') }}
            </a-button>
            <a-popconfirm
              :content="t('devicePlatform.projects.confirm.rotateApiKey')"
              :disabled="credentialRotation.loading"
              @ok="handleRotateAPIKey(record)"
            >
              <a-button
                type="text"
                size="small"
                :disabled="credentialRotation.loading"
                :loading="credentialRotation.projectId === record.id && credentialRotation.kind === 'api-key'"
              >
                {{ t('devicePlatform.projects.action.rotateApiKey') }}
              </a-button>
            </a-popconfirm>
            <a-popconfirm
              :content="t('devicePlatform.projects.confirm.rotateWebhookSecret')"
              :disabled="!record.webhook_configured || credentialRotation.loading"
              @ok="handleRotateWebhookSecret(record)"
            >
              <a-button
                type="text"
                size="small"
                :disabled="!record.webhook_configured || credentialRotation.loading"
                :loading="credentialRotation.projectId === record.id && credentialRotation.kind === 'webhook-secret'"
              >
                {{ t('devicePlatform.projects.action.rotateWebhookSecret') }}
              </a-button>
            </a-popconfirm>
          </a-space>
        </template>
      </GridTable>
    </Grid>

    <a-modal
      v-model:visible="editVisible"
      :title="t('devicePlatform.projects.edit.title')"
      width="min(calc(100vw - 24px), 520px)"
      :ok-loading="loading"
      @before-ok="handleUpdateProject"
    >
      <a-form :model="editForm" layout="vertical">
        <a-form-item :label="t('devicePlatform.projects.form.name')">
          <a-input v-model="editForm.name" />
        </a-form-item>
        <a-form-item :label="t('devicePlatform.projects.form.webhook')">
          <a-input v-model="editForm.webhook_url" :placeholder="t('devicePlatform.projects.form.webhook.placeholder')" />
        </a-form-item>
        <a-form-item :label="t('devicePlatform.projects.form.whitelist')">
          <a-input v-model="editForm.ip_whitelist" :placeholder="t('devicePlatform.projects.form.whitelist.placeholder')" />
        </a-form-item>
      </a-form>
    </a-modal>

    <a-modal
      v-model:visible="credentialVisible"
      :title="t('devicePlatform.projects.credential.title')"
      width="min(calc(100vw - 24px), 520px)"
      :footer="false"
      :unmount-on-close="true"
      @close="clearCredential"
    >
      <a-alert type="warning">{{ t('devicePlatform.projects.credential.warning') }}</a-alert>
      <a-descriptions :column="1" bordered class="dp-panel-title">
        <a-descriptions-item v-if="issuedCredential?.api_key" label="API Key">
          <a-typography-text class="dp-monospace" copyable>{{ issuedCredential.api_key }}</a-typography-text>
        </a-descriptions-item>
        <a-descriptions-item v-if="issuedCredential?.webhook_secret" label="Webhook Secret">
          <a-typography-text class="dp-monospace" copyable>{{ issuedCredential.webhook_secret }}</a-typography-text>
        </a-descriptions-item>
      </a-descriptions>
    </a-modal>
  </div>
</template>

<script lang="ts" setup>
  import { computed, onMounted, reactive, ref, watch } from 'vue';
  import { useI18n } from 'vue-i18n';
  import { Message } from '@arco-design/web-vue';
  import useLoading from '@/hooks/loading';
  import {
    ProjectCredentialRecord,
    ProjectRecord,
    createProject,
    queryProjects,
    rotateProjectAPIKey,
    rotateProjectWebhookSecret,
    updateProject,
  } from '@/api/device-platform';
  import '../device-platform/workspace.less';

  defineOptions({ name: 'ProjectsIndex' });

  const { t } = useI18n();
  const { loading, setLoading } = useLoading(false);
  const projects = ref<ProjectRecord[]>([]);
  const editingProject = ref<ProjectRecord>();
  const issuedCredential = ref<ProjectCredentialRecord>();
  const editVisible = ref(false);
  const credentialVisible = ref(false);
  const projectWhitelist = ref('');
  const projectForm = reactive({
    name: '',
    webhook_url: '',
  });
  const editForm = reactive({ name: '', webhook_url: '', ip_whitelist: '' });
  const credentialRotation = reactive({
    loading: false,
    projectId: '',
    kind: '' as 'api-key' | 'webhook-secret' | '',
  });
  const pagination = reactive({
    current: 1,
    pageSize: 10,
    total: 0,
    showTotal: true,
    showPageSize: true,
  });

  const webhookCount = computed(() => projects.value.filter((project) => !!project.webhook_url).length);
  const whitelistCount = computed(() => projects.value.filter((project) => project.ip_whitelist?.length).length);
  const webhookDisabledCount = computed(() => projects.value.filter((project) => !project.webhook_configured).length);
  const columns = computed(() => [
    { title: t('devicePlatform.projects.columns.name'), slotName: 'project', width: 260 },
    { title: t('devicePlatform.projects.columns.webhook'), slotName: 'webhook', width: 320 },
    { title: t('devicePlatform.projects.columns.whitelist'), slotName: 'whitelist', width: 220 },
    { title: t('devicePlatform.common.createdAt'), dataIndex: 'created_at', ellipsis: true, tooltip: true, width: 190 },
    { title: t('devicePlatform.common.actions'), slotName: 'action', width: 310, fixed: 'right' },
  ]);

  const showCredential = (credential: ProjectCredentialRecord) => {
    issuedCredential.value = undefined;
    if (!credential.api_key && !credential.webhook_secret) return;
    issuedCredential.value = credential;
    credentialVisible.value = true;
  };

  const clearCredential = () => {
    issuedCredential.value = undefined;
  };

  watch(credentialVisible, (visible) => {
    if (!visible) clearCredential();
  });

  const parseWhitelist = (value: string) =>
    value
      .split(',')
      .map((item) => item.trim())
      .filter(Boolean);

  const refresh = async () => {
    setLoading(true);
    try {
      const res = await queryProjects({ page: pagination.current, page_size: pagination.pageSize });
      projects.value = res.data;
      pagination.total = res.meta?.total ?? res.data.length;
    } finally {
      setLoading(false);
    }
  };

  const onPageChange = (page: number) => {
    pagination.current = page;
    refresh();
  };

  const onPageSizeChange = (pageSize: number) => {
    pagination.pageSize = pageSize;
    pagination.current = 1;
    refresh();
  };

  const handleCreateProject = async () => {
    if (!projectForm.name.trim()) {
      Message.warning(t('devicePlatform.projects.message.nameRequired'));
      return;
    }
    setLoading(true);
    try {
      const res = await createProject({
        name: projectForm.name.trim(),
        ...(projectForm.webhook_url.trim() ? { webhook_url: projectForm.webhook_url.trim() } : {}),
        ip_whitelist: parseWhitelist(projectWhitelist.value),
      });
      projectForm.name = '';
      projectForm.webhook_url = '';
      projectWhitelist.value = '';
      pagination.current = 1;
      await refresh();
      showCredential(res.data);
      Message.success(t('devicePlatform.projects.message.created'));
    } finally {
      setLoading(false);
    }
  };

  const openEditModal = (record: ProjectRecord) => {
    editingProject.value = record;
    editForm.name = record.name;
    editForm.webhook_url = record.webhook_url || '';
    editForm.ip_whitelist = record.ip_whitelist.join(', ');
    editVisible.value = true;
  };

  const handleUpdateProject = async (done: (closed: boolean) => void) => {
    if (!editingProject.value || !editForm.name.trim()) {
      Message.warning(t('devicePlatform.projects.message.nameRequired'));
      done(false);
      return;
    }
    setLoading(true);
    try {
      const res = await updateProject(editingProject.value.id, {
        name: editForm.name.trim(),
        webhook_url: editForm.webhook_url.trim() || null,
        ip_whitelist: parseWhitelist(editForm.ip_whitelist),
      });
      await refresh();
      editVisible.value = false;
      showCredential(res.data);
      Message.success(t('devicePlatform.projects.message.updated'));
      done(true);
    } catch {
      done(false);
    } finally {
      setLoading(false);
    }
  };

  const handleRotateAPIKey = async (record: ProjectRecord) => {
    if (credentialRotation.loading) return;
    credentialRotation.loading = true;
    credentialRotation.projectId = record.id;
    credentialRotation.kind = 'api-key';
    try {
      const res = await rotateProjectAPIKey(record.id);
      showCredential(res.data);
      Message.success(t('devicePlatform.projects.message.apiKeyRotated'));
    } finally {
      credentialRotation.loading = false;
      credentialRotation.projectId = '';
      credentialRotation.kind = '';
    }
  };

  const handleRotateWebhookSecret = async (record: ProjectRecord) => {
    if (credentialRotation.loading) return;
    credentialRotation.loading = true;
    credentialRotation.projectId = record.id;
    credentialRotation.kind = 'webhook-secret';
    try {
      const res = await rotateProjectWebhookSecret(record.id);
      showCredential(res.data);
      Message.success(t('devicePlatform.projects.message.webhookSecretRotated'));
    } finally {
      credentialRotation.loading = false;
      credentialRotation.projectId = '';
      credentialRotation.kind = '';
    }
  };

  onMounted(refresh);
</script>
