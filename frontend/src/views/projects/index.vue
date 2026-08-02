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
          <a-button v-if="isSuperAdmin" type="primary" data-testid="create-project" @click="openCreateModal">
            <template #icon><icon-plus /></template>
            {{ t('devicePlatform.projects.action.create') }}
          </a-button>
        </a-space>
      </div>
      <div class="dp-metrics">
        <div class="dp-metric">
          <div class="dp-metric-label">{{ t('devicePlatform.projects.metric.total') }}</div>
          <div class="dp-metric-value">{{ pagination.total }}</div>
        </div>
        <div class="dp-metric is-green">
          <div class="dp-metric-label">{{ t('devicePlatform.projects.metric.managers') }}</div>
          <div class="dp-metric-value">{{ managerCount }}</div>
        </div>
        <div v-if="isSuperAdmin" class="dp-metric is-orange">
          <div class="dp-metric-label">{{ t('devicePlatform.projects.metric.webhook') }}</div>
          <div class="dp-metric-value">{{ webhookCount }}</div>
        </div>
        <div v-if="isSuperAdmin" class="dp-metric is-purple">
          <div class="dp-metric-label">{{ t('devicePlatform.projects.metric.whitelist') }}</div>
          <div class="dp-metric-value">{{ whitelistCount }}</div>
        </div>
      </div>
    </section>

    <Grid :title="t('devicePlatform.projects.table.title')">
      <GridToolbar @refresh="refresh">
        <template #prepend>
          <a-space wrap>
            <a-input
              v-model="filters.name"
              class="dp-toolbar-control"
              allow-clear
              :placeholder="t('devicePlatform.projects.filter.name')"
              @press-enter="handleSearch"
            />
            <a-select
              v-if="isSuperAdmin"
              v-model="filters.manager_user_id"
              class="dp-toolbar-control is-wide"
              allow-clear
              allow-search
              :loading="userOptions.loading"
              :placeholder="t('devicePlatform.projects.filter.manager')"
              @change="handleSearch"
              @dropdown-reach-bottom="loadMoreUserOptions"
            >
              <a-option v-for="user in userOptions.items" :key="user.id" :value="user.id">
                {{ user.display_name }} ({{ user.email }})
              </a-option>
            </a-select>
            <a-button type="primary" @click="handleSearch">
              <template #icon><icon-search /></template>
              {{ t('devicePlatform.projects.action.search') }}
            </a-button>
            <a-button @click="handleReset">
              <template #icon><icon-refresh /></template>
              {{ t('devicePlatform.projects.action.reset') }}
            </a-button>
          </a-space>
        </template>
      </GridToolbar>

      <GridTable
        class="dp-table"
        :loading="loading"
        :data="projects"
        :columns="columns"
        :pagination="pagination"
        :scroll="{ x: isSuperAdmin ? 1460 : 880 }"
        @page-change="onPageChange"
        @page-size-change="onPageSizeChange"
      >
        <template #project="{ record }">
          <div class="dp-cell-primary">{{ record.name }}</div>
          <div class="dp-cell-secondary dp-monospace">{{ record.id }}</div>
        </template>
        <template #manager="{ record }">
          <div class="dp-cell-primary">{{ record.manager.display_name }}</div>
          <div class="dp-cell-secondary">{{ record.manager.email }}</div>
          <a-tag :color="record.manager.status === 'active' ? 'green' : 'gray'" size="small">
            {{ t(`devicePlatform.users.status.${record.manager.status}`) }}
          </a-tag>
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
          <a-space wrap>
            <a-button type="text" size="small" @click="openEditModal(record)">
              {{ t('devicePlatform.projects.action.edit') }}
            </a-button>
            <a-button v-if="isSuperAdmin" type="text" size="small" @click="openTransferModal(record)">
              {{ t('devicePlatform.projects.action.transfer') }}
            </a-button>
            <a-popconfirm
              v-if="isSuperAdmin"
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
              v-if="isSuperAdmin"
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
      v-model:visible="createVisible"
      :title="t('devicePlatform.projects.create.title')"
      width="min(calc(100vw - 24px), 560px)"
      :ok-loading="loading"
      :unmount-on-close="true"
      @before-ok="handleCreateProject"
      @close="resetCreateForm"
    >
      <a-form :model="createForm" layout="vertical">
        <a-form-item :label="t('devicePlatform.projects.form.name')" required>
          <a-input v-model="createForm.name" data-testid="project-name" />
        </a-form-item>
        <a-form-item :label="t('devicePlatform.projects.form.manager')" required>
          <a-select
            v-model="createForm.manager_user_id"
            data-testid="project-manager"
            allow-search
            :loading="userOptions.loading"
            :placeholder="t('devicePlatform.projects.form.manager.placeholder')"
            @dropdown-reach-bottom="loadMoreUserOptions"
          >
            <a-option v-for="user in activeUserOptions" :key="user.id" :value="user.id">
              {{ user.display_name }} ({{ user.email }})
            </a-option>
          </a-select>
        </a-form-item>
        <a-form-item :label="t('devicePlatform.projects.form.webhook')">
          <a-input
            v-model="createForm.webhook_url"
            data-testid="project-webhook-url"
            :placeholder="t('devicePlatform.projects.form.webhook.placeholder')"
          />
        </a-form-item>
        <a-form-item :label="t('devicePlatform.projects.form.whitelist')">
          <a-input
            v-model="createForm.ip_whitelist"
            data-testid="project-ip-whitelist"
            :placeholder="t('devicePlatform.projects.form.whitelist.placeholder')"
          />
        </a-form-item>
      </a-form>
    </a-modal>

    <a-modal
      v-model:visible="editVisible"
      :title="t('devicePlatform.projects.edit.title')"
      width="min(calc(100vw - 24px), 520px)"
      :ok-loading="loading"
      @before-ok="handleUpdateProject"
    >
      <a-form :model="editForm" layout="vertical">
        <a-form-item :label="t('devicePlatform.projects.form.name')" required>
          <a-input v-model="editForm.name" data-testid="edit-project-name" />
        </a-form-item>
        <a-form-item v-if="isSuperAdmin" :label="t('devicePlatform.projects.form.webhook')">
          <a-input v-model="editForm.webhook_url" :placeholder="t('devicePlatform.projects.form.webhook.placeholder')" />
        </a-form-item>
        <a-form-item v-if="isSuperAdmin" :label="t('devicePlatform.projects.form.whitelist')">
          <a-input v-model="editForm.ip_whitelist" :placeholder="t('devicePlatform.projects.form.whitelist.placeholder')" />
        </a-form-item>
      </a-form>
    </a-modal>

    <a-modal
      v-model:visible="transferVisible"
      :title="t('devicePlatform.projects.transfer.title')"
      width="min(calc(100vw - 24px), 520px)"
      :ok-loading="transferLoading"
      @before-ok="handleTransferProject"
    >
      <a-alert type="warning">{{ t('devicePlatform.projects.transfer.warning') }}</a-alert>
      <a-form :model="transferForm" layout="vertical" class="dp-panel-title">
        <a-form-item :label="t('devicePlatform.projects.form.manager')" required>
          <a-select
            v-model="transferForm.manager_user_id"
            data-testid="transfer-project-manager"
            allow-search
            :loading="userOptions.loading"
            @dropdown-reach-bottom="loadMoreUserOptions"
          >
            <a-option v-for="user in activeUserOptions" :key="user.id" :value="user.id">
              {{ user.display_name }} ({{ user.email }})
            </a-option>
          </a-select>
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
  import { Message, TableColumnData } from '@arco-design/web-vue';
  import useLoading from '@/hooks/loading';
  import { useUserStore } from '@/store';
  import { UserRecord, queryUsers } from '@/api/user';
  import {
    ProjectCredentialRecord,
    ProjectRecord,
    createProject,
    queryProjects,
    rotateProjectAPIKey,
    rotateProjectWebhookSecret,
    transferProject,
    updateProject,
  } from '@/api/device-platform';
  import '../device-platform/workspace.less';

  defineOptions({ name: 'ProjectsIndex' });

  const { t } = useI18n();
  const userStore = useUserStore();
  const { loading, setLoading } = useLoading(false);
  const isSuperAdmin = computed(() => userStore.is_super_admin);
  const projects = ref<ProjectRecord[]>([]);
  const editingProject = ref<ProjectRecord>();
  const transferringProject = ref<ProjectRecord>();
  const issuedCredential = ref<ProjectCredentialRecord>();
  const createVisible = ref(false);
  const editVisible = ref(false);
  const transferVisible = ref(false);
  const transferLoading = ref(false);
  const credentialVisible = ref(false);
  const filters = reactive({ name: '', manager_user_id: '' });
  const createForm = reactive({ name: '', manager_user_id: '', webhook_url: '', ip_whitelist: '' });
  const editForm = reactive({ name: '', webhook_url: '', ip_whitelist: '' });
  const transferForm = reactive({ manager_user_id: '' });
  const credentialRotation = reactive({
    loading: false,
    projectId: '',
    kind: '' as 'api-key' | 'webhook-secret' | '',
  });
  const userOptions = reactive({
    items: [] as UserRecord[],
    page: 0,
    total: 0,
    loading: false,
  });
  const pagination = reactive({
    current: 1,
    pageSize: 10,
    total: 0,
    showTotal: true,
    showPageSize: true,
  });

  const activeUserOptions = computed(() => userOptions.items.filter((user) => user.status === 'active'));
  const managerCount = computed(() => new Set(projects.value.map((project) => project.manager_user_id)).size);
  const webhookCount = computed(() => projects.value.filter((project) => project.webhook_configured).length);
  const whitelistCount = computed(() => projects.value.filter((project) => project.ip_whitelist?.length).length);
  const columns = computed<TableColumnData[]>(() => {
    const base: TableColumnData[] = [
      { title: t('devicePlatform.projects.columns.name'), slotName: 'project', width: 260 },
      { title: t('devicePlatform.projects.columns.manager'), slotName: 'manager', width: 280 },
    ];
    if (isSuperAdmin.value) {
      base.push(
        { title: t('devicePlatform.projects.columns.webhook'), slotName: 'webhook', width: 320 },
        { title: t('devicePlatform.projects.columns.whitelist'), slotName: 'whitelist', width: 220 }
      );
    }
    base.push(
      { title: t('devicePlatform.common.createdAt'), dataIndex: 'created_at', ellipsis: true, tooltip: true, width: 190 },
      {
        title: t('devicePlatform.common.actions'),
        slotName: 'action',
        width: isSuperAdmin.value ? 390 : 120,
        fixed: 'right',
      }
    );
    return base;
  });

  const parseWhitelist = (value: string) =>
    value
      .split(',')
      .map((item) => item.trim())
      .filter(Boolean);

  const loadMoreUserOptions = async () => {
    const hasLoadedAll = userOptions.page > 0 && userOptions.items.length >= userOptions.total;
    if (!isSuperAdmin.value || userOptions.loading || hasLoadedAll) return;
    userOptions.loading = true;
    try {
      const nextPage = userOptions.page + 1;
      const res = await queryUsers({ page: nextPage, page_size: 50 });
      userOptions.items.push(...res.data.filter((item) => !userOptions.items.some((current) => current.id === item.id)));
      userOptions.page = nextPage;
      userOptions.total = res.meta?.total ?? userOptions.items.length;
    } finally {
      userOptions.loading = false;
    }
  };

  const refresh = async () => {
    setLoading(true);
    try {
      const res = await queryProjects({
        page: pagination.current,
        page_size: pagination.pageSize,
        ...(filters.name.trim() ? { name: filters.name.trim() } : {}),
        ...(isSuperAdmin.value && filters.manager_user_id ? { manager_user_id: filters.manager_user_id } : {}),
      });
      projects.value = res.data;
      pagination.total = res.meta?.total ?? res.data.length;
    } finally {
      setLoading(false);
    }
  };

  const handleSearch = () => {
    pagination.current = 1;
    refresh();
  };

  const handleReset = () => {
    filters.name = '';
    filters.manager_user_id = '';
    handleSearch();
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

  const resetCreateForm = () => {
    createForm.name = '';
    createForm.manager_user_id = '';
    createForm.webhook_url = '';
    createForm.ip_whitelist = '';
  };

  const openCreateModal = () => {
    resetCreateForm();
    createVisible.value = true;
  };

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

  const handleCreateProject = async (done: (closed: boolean) => void) => {
    if (!createForm.name.trim() || !createForm.manager_user_id) {
      Message.warning(t('devicePlatform.projects.message.required'));
      done(false);
      return;
    }
    setLoading(true);
    try {
      const res = await createProject({
        name: createForm.name.trim(),
        manager_user_id: createForm.manager_user_id,
        ...(createForm.webhook_url.trim() ? { webhook_url: createForm.webhook_url.trim() } : {}),
        ip_whitelist: parseWhitelist(createForm.ip_whitelist),
      });
      pagination.current = 1;
      await refresh();
      showCredential(res.data);
      Message.success(t('devicePlatform.projects.message.created'));
      done(true);
    } catch {
      done(false);
    } finally {
      setLoading(false);
    }
  };

  const openEditModal = (record: ProjectRecord) => {
    editingProject.value = record;
    editForm.name = record.name;
    editForm.webhook_url = record.webhook_url || '';
    editForm.ip_whitelist = record.ip_whitelist?.join(', ') || '';
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
      const data = isSuperAdmin.value
        ? {
            name: editForm.name.trim(),
            webhook_url: editForm.webhook_url.trim() || null,
            ip_whitelist: parseWhitelist(editForm.ip_whitelist),
          }
        : { name: editForm.name.trim() };
      const res = await updateProject(editingProject.value.id, data);
      await refresh();
      showCredential(res.data);
      Message.success(t('devicePlatform.projects.message.updated'));
      done(true);
    } catch {
      done(false);
    } finally {
      setLoading(false);
    }
  };

  const openTransferModal = (record: ProjectRecord) => {
    transferringProject.value = record;
    transferForm.manager_user_id = record.manager_user_id;
    transferVisible.value = true;
  };

  const handleTransferProject = async (done: (closed: boolean) => void) => {
    if (!transferringProject.value || !transferForm.manager_user_id) {
      Message.warning(t('devicePlatform.projects.message.managerRequired'));
      done(false);
      return;
    }
    transferLoading.value = true;
    try {
      await transferProject(transferringProject.value.id, transferForm.manager_user_id);
      await refresh();
      Message.success(t('devicePlatform.projects.message.transferred'));
      done(true);
    } catch {
      done(false);
    } finally {
      transferLoading.value = false;
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

  onMounted(async () => {
    await refresh();
    if (isSuperAdmin.value) await loadMoreUserOptions();
  });
</script>
