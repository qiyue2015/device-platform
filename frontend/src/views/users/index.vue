<template>
  <div class="page-container device-platform-page">
    <section class="dp-overview">
      <div class="dp-overview-main">
        <div class="dp-kicker">{{ t('devicePlatform.kicker') }}</div>
        <h1 class="dp-title">{{ t('devicePlatform.users.title') }}</h1>
        <p class="dp-description">{{ t('devicePlatform.users.description') }}</p>
      </div>
      <div class="dp-overview-actions">
        <a-space>
          <a-button @click="refresh">
            <template #icon><icon-refresh /></template>
            {{ t('devicePlatform.common.refresh') }}
          </a-button>
          <a-button type="primary" data-testid="create-user" @click="openCreateModal">
            <template #icon><icon-plus /></template>
            {{ t('devicePlatform.users.action.create') }}
          </a-button>
        </a-space>
      </div>
      <div class="dp-metrics">
        <div class="dp-metric">
          <div class="dp-metric-label">{{ t('devicePlatform.users.metric.total') }}</div>
          <div class="dp-metric-value">{{ pagination.total }}</div>
        </div>
        <div class="dp-metric is-green">
          <div class="dp-metric-label">{{ t('devicePlatform.users.metric.active') }}</div>
          <div class="dp-metric-value">{{ activeCount }}</div>
        </div>
        <div class="dp-metric is-orange">
          <div class="dp-metric-label">{{ t('devicePlatform.users.metric.disabled') }}</div>
          <div class="dp-metric-value">{{ disabledCount }}</div>
        </div>
        <div class="dp-metric is-purple">
          <div class="dp-metric-label">{{ t('devicePlatform.users.metric.regular') }}</div>
          <div class="dp-metric-value">{{ regularCount }}</div>
        </div>
      </div>
    </section>

    <Grid :title="t('devicePlatform.users.table.title')">
      <GridToolbar @refresh="refresh">
        <template #prepend>
          <a-space wrap>
            <a-input
              v-model="filters.email"
              class="dp-toolbar-control"
              allow-clear
              :placeholder="t('devicePlatform.users.filter.email')"
              @press-enter="handleSearch"
            />
            <a-select
              v-model="filters.status"
              class="dp-toolbar-control"
              allow-clear
              :placeholder="t('devicePlatform.users.filter.status')"
              @change="handleSearch"
            >
              <a-option value="active">{{ t('devicePlatform.users.status.active') }}</a-option>
              <a-option value="disabled">{{ t('devicePlatform.users.status.disabled') }}</a-option>
            </a-select>
            <a-button type="primary" @click="handleSearch">
              <template #icon><icon-search /></template>
              {{ t('devicePlatform.users.action.search') }}
            </a-button>
            <a-button @click="handleReset">
              <template #icon><icon-refresh /></template>
              {{ t('devicePlatform.users.action.reset') }}
            </a-button>
          </a-space>
        </template>
      </GridToolbar>

      <GridTable
        class="dp-table"
        :loading="loading"
        :data="users"
        :columns="columns"
        :pagination="pagination"
        :scroll="{ x: 960 }"
        @page-change="onPageChange"
        @page-size-change="onPageSizeChange"
      >
        <template #identity="{ record }">
          <div class="dp-cell-primary">{{ record.display_name }}</div>
          <div class="dp-cell-secondary">{{ record.email }}</div>
          <div class="dp-cell-secondary dp-monospace">{{ record.id }}</div>
        </template>
        <template #role="{ record }">
          <a-tag :color="record.is_super_admin ? 'orangered' : 'arcoblue'">
            {{
              record.is_super_admin ? t('devicePlatform.users.role.superAdmin') : t('devicePlatform.users.role.projectManager')
            }}
          </a-tag>
        </template>
        <template #status="{ record }">
          <a-tag :color="record.status === 'active' ? 'green' : 'gray'">
            {{ t(`devicePlatform.users.status.${record.status}`) }}
          </a-tag>
        </template>
        <template #action="{ record }">
          <span v-if="record.is_super_admin" class="dp-muted">{{ t('devicePlatform.users.action.immutable') }}</span>
          <a-popconfirm
            v-else
            :content="
              record.status === 'active' ? t('devicePlatform.users.confirm.disable') : t('devicePlatform.users.confirm.enable')
            "
            @ok="handleStatusChange(record)"
          >
            <a-button type="text" size="small" :loading="statusMutation.userId === record.id">
              {{
                record.status === 'active' ? t('devicePlatform.users.action.disable') : t('devicePlatform.users.action.enable')
              }}
            </a-button>
          </a-popconfirm>
        </template>
      </GridTable>
    </Grid>

    <a-modal
      v-model:visible="createVisible"
      :title="t('devicePlatform.users.create.title')"
      width="min(calc(100vw - 24px), 520px)"
      :ok-loading="createLoading"
      :unmount-on-close="true"
      @before-ok="handleCreateUser"
      @close="resetCreateForm"
    >
      <a-form :model="createForm" layout="vertical">
        <a-form-item :label="t('devicePlatform.users.form.email')" required>
          <a-input v-model="createForm.email" data-testid="user-email" autocomplete="off" />
        </a-form-item>
        <a-form-item :label="t('devicePlatform.users.form.displayName')" required>
          <a-input v-model="createForm.display_name" data-testid="user-display-name" />
        </a-form-item>
        <a-form-item :label="t('devicePlatform.users.form.password')" required>
          <a-input-password v-model="createForm.password" data-testid="user-password" autocomplete="new-password" />
        </a-form-item>
        <a-form-item :label="t('devicePlatform.users.form.confirmPassword')" required>
          <a-input-password
            v-model="createForm.confirm_password"
            data-testid="user-confirm-password"
            autocomplete="new-password"
          />
        </a-form-item>
      </a-form>
    </a-modal>
  </div>
</template>

<script lang="ts" setup>
  import { computed, onMounted, reactive, ref } from 'vue';
  import { useI18n } from 'vue-i18n';
  import { Message } from '@arco-design/web-vue';
  import useLoading from '@/hooks/loading';
  import { UserRecord, UserStatus, createUser, queryUsers, updateUserStatus } from '@/api/user';
  import '../device-platform/workspace.less';

  defineOptions({ name: 'UsersIndex' });

  const { t } = useI18n();
  const { loading, setLoading } = useLoading(false);
  const users = ref<UserRecord[]>([]);
  const createVisible = ref(false);
  const createLoading = ref(false);
  const statusMutation = reactive({ userId: '' });
  const filters = reactive<{ email: string; status?: UserStatus }>({ email: '', status: undefined });
  const createForm = reactive({ email: '', display_name: '', password: '', confirm_password: '' });
  const pagination = reactive({
    current: 1,
    pageSize: 10,
    total: 0,
    showTotal: true,
    showPageSize: true,
  });

  const activeCount = computed(() => users.value.filter((user) => user.status === 'active').length);
  const disabledCount = computed(() => users.value.filter((user) => user.status === 'disabled').length);
  const regularCount = computed(() => users.value.filter((user) => !user.is_super_admin).length);
  const columns = computed(() => [
    { title: t('devicePlatform.users.columns.user'), slotName: 'identity', width: 320 },
    { title: t('devicePlatform.users.columns.role'), slotName: 'role', width: 180 },
    { title: t('devicePlatform.common.status'), slotName: 'status', width: 130 },
    { title: t('devicePlatform.common.createdAt'), dataIndex: 'created_at', width: 200 },
    { title: t('devicePlatform.common.actions'), slotName: 'action', width: 140, fixed: 'right' },
  ]);

  const refresh = async () => {
    setLoading(true);
    try {
      const res = await queryUsers({
        page: pagination.current,
        page_size: pagination.pageSize,
        ...(filters.email.trim() ? { email: filters.email.trim() } : {}),
        ...(filters.status ? { status: filters.status } : {}),
      });
      users.value = res.data;
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
    filters.email = '';
    filters.status = undefined;
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
    createForm.email = '';
    createForm.display_name = '';
    createForm.password = '';
    createForm.confirm_password = '';
  };

  const openCreateModal = () => {
    resetCreateForm();
    createVisible.value = true;
  };

  const handleCreateUser = async (done: (closed: boolean) => void) => {
    if (!createForm.email.trim() || createForm.display_name.trim().length < 2 || createForm.password.length < 8) {
      Message.warning(t('devicePlatform.users.message.invalidForm'));
      done(false);
      return;
    }
    if (createForm.password !== createForm.confirm_password) {
      Message.warning(t('devicePlatform.users.message.passwordMismatch'));
      done(false);
      return;
    }
    createLoading.value = true;
    try {
      await createUser({
        email: createForm.email.trim(),
        display_name: createForm.display_name.trim(),
        password: createForm.password,
      });
      pagination.current = 1;
      await refresh();
      Message.success(t('devicePlatform.users.message.created'));
      done(true);
    } catch {
      done(false);
    } finally {
      createLoading.value = false;
    }
  };

  const handleStatusChange = async (record: UserRecord) => {
    if (statusMutation.userId) return;
    statusMutation.userId = record.id;
    try {
      const nextStatus: UserStatus = record.status === 'active' ? 'disabled' : 'active';
      await updateUserStatus(record.id, nextStatus);
      await refresh();
      Message.success(
        nextStatus === 'active' ? t('devicePlatform.users.message.enabled') : t('devicePlatform.users.message.disabled')
      );
    } finally {
      statusMutation.userId = '';
    }
  };

  onMounted(refresh);
</script>
