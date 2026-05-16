<template>
  <div class="page-container">
    <Grid :title="$t('menu.system.role')">
      <GridToolbar @create="handleCreate" @refresh="fetchData" />
      <GridTable
        :loading="loading"
        :data="tableData"
        :columns="columns"
        :pagination="pagination"
        :disable-delete="isSuperAdmin"
        @edit="handleEdit"
        @delete="handleDelete"
        @page-change="onPageChange"
        @page-size-change="onPageSizeChange"
      />
      <EditRoleDrawer ref="editDrawerRef" @success="fetchData" />
    </Grid>
  </div>
</template>

<script lang="ts" setup>
  import { ref, reactive, computed, onMounted } from 'vue';
  import { useI18n } from 'vue-i18n';
  import { Modal, Message } from '@arco-design/web-vue';
  import { useLoading } from '@/hooks';
  import { HttpResponse } from '@/api/interceptor';
  import { queryRoleList, deleteRole, type RoleRecord } from '@/api/system/role';
  import { queryMenuTree, type MenuRecord } from '@/api/system/menu';
  import EditRoleDrawer from './components/EditRoleDrawer.vue';

  defineOptions({ name: 'SystemRole' });

  const { t } = useI18n();
  const { loading, setLoading } = useLoading(false);
  const tableData = ref<RoleRecord[]>([]);
  const menus = ref<MenuRecord[]>([]);
  const editDrawerRef = ref<InstanceType<typeof EditRoleDrawer>>();

  const pagination = reactive({
    current: 1,
    pageSize: 10,
    total: 0,
    showTotal: true,
    showPageSize: true,
  });

  const columns = computed(() => [
    { title: t('system.role.columns.id'), dataIndex: 'id', width: 80 },
    { title: t('system.role.columns.name'), dataIndex: 'name' },
    { title: t('system.role.columns.createdAt'), dataIndex: 'created_at', width: 180 },
    { title: t('system.role.columns.operations'), slotName: 'action', width: 120 },
  ]);

  const fetchData = async () => {
    setLoading(true);
    try {
      const res = (await queryRoleList({
        current: pagination.current,
        pageSize: pagination.pageSize,
      })) as unknown as HttpResponse<RoleRecord[]>;
      tableData.value = res.data;
      pagination.total = res.meta?.total ?? 0;
    } finally {
      setLoading(false);
    }
  };

  const fetchMenus = async () => {
    try {
      const res = await queryMenuTree();
      menus.value = res.data;
    } catch {
      // silent
    }
  };

  const onPageChange = (page: number) => {
    pagination.current = page;
    fetchData();
  };

  const onPageSizeChange = (pageSize: number) => {
    pagination.pageSize = pageSize;
    pagination.current = 1;
    fetchData();
  };

  const isSuperAdmin = (record: RoleRecord) => record.name === 'super-admin';

  const handleCreate = () => {
    editDrawerRef.value?.onCreate(menus.value);
  };

  const handleEdit = (record: RoleRecord) => {
    editDrawerRef.value?.onEdit(record, menus.value);
  };

  const handleDelete = (record: RoleRecord) => {
    Modal.warning({
      title: t('system.role.delete.title'),
      content: t('system.role.delete.content'),
      onOk: async () => {
        await deleteRole(record.id);
        Message.success(t('system.role.delete.success'));
        fetchData();
      },
    });
  };

  onMounted(() => {
    fetchData();
    fetchMenus();
  });
</script>
