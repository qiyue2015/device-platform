<template>
  <div class="page-container">
    <Grid :title="$t('menu.system.menu')">
      <GridToolbar @create="handleCreate" @refresh="fetchData">
        <template #extra>
          <a-tooltip :content="isAllExpanded ? $t('system.menu.collapseAll') : $t('system.menu.expandAll')">
            <a-button @click="toggleExpandAll">
              <template #icon>
                <icon-menu-fold v-if="isAllExpanded" />
                <icon-menu-unfold v-else />
              </template>
            </a-button>
          </a-tooltip>
        </template>
      </GridToolbar>
      <GridTable
        v-model:expanded-keys="expandedKeys"
        :loading="loading"
        :data="tableData"
        :columns="columns"
        :pagination="false"
        row-key="id"
      >
        <template #name="{ record }">
          <div class="menu-name-cell">
            <component :is="record.icon" v-if="record.icon" class="menu-icon" />
            <div>
              <div class="menu-name-text">{{ getMenuLabel(record) }}</div>
              <div class="menu-name-sub">{{ record.name }}</div>
            </div>
            <a-tag v-if="record.is_hidden" size="small" color="gray">
              {{ $t('system.menu.hidden') }}
            </a-tag>
          </div>
        </template>
        <template #type="{ record }">
          <a-tag v-if="record.type === 1" color="arcoblue" size="small">{{ $t('system.menu.types.directory') }}</a-tag>
          <a-tag v-else-if="record.type === 2" color="green" size="small">{{ $t('system.menu.types.menu') }}</a-tag>
          <a-tag v-else-if="record.type === 3" color="orangered" size="small">{{ $t('system.menu.types.button') }}</a-tag>
        </template>
        <template #permission="{ record }">
          <code v-if="record.type === 3 && record.permission" class="menu-code">{{ record.permission }}</code>
          <span v-else-if="record.type === 2 && record.component" class="menu-component">{{ record.component }}</span>
          <span v-else class="menu-empty">-</span>
        </template>
        <template #status="{ record }">
          <a-badge v-if="record.is_active" status="success" :text="$t('system.menu.status.active')" />
          <a-badge v-else status="danger" :text="$t('system.menu.status.inactive')" />
        </template>
        <template #action="{ record }">
          <a-space>
            <a-tooltip v-if="record.type !== 3" :content="$t('system.menu.addChild')" mini>
              <a-button type="text" size="small" @click="handleCreateChild(record)">
                <template #icon><icon-plus /></template>
              </a-button>
            </a-tooltip>
            <a-tooltip :content="$t('system.menu.editModal.titleEdit')" mini>
              <a-button type="text" size="small" @click="handleEdit(record)">
                <template #icon><icon-edit /></template>
              </a-button>
            </a-tooltip>
            <a-tooltip :content="$t('system.menu.delete.title')" mini>
              <a-button type="text" size="small" status="danger" @click="handleDelete(record)">
                <template #icon><icon-delete /></template>
              </a-button>
            </a-tooltip>
          </a-space>
        </template>
      </GridTable>
      <EditMenuModal ref="editModalRef" @success="onEditSuccess" />
    </Grid>
  </div>
</template>

<script lang="ts" setup>
  import { ref, computed, onMounted } from 'vue';
  import { useI18n } from 'vue-i18n';
  import { Modal, Message } from '@arco-design/web-vue';
  import { useLoading } from '@/hooks';
  import { useAppStore } from '@/store';
  import { queryMenuTree, deleteMenu, type MenuRecord } from '@/api/system/menu';
  import EditMenuModal from './components/EditMenuModal.vue';

  defineOptions({ name: 'SystemMenu' });

  const { t } = useI18n();
  const appStore = useAppStore();
  const { loading, setLoading } = useLoading(false);
  const tableData = ref<MenuRecord[]>([]);
  const expandedKeys = ref<number[]>([]);
  const editModalRef = ref<InstanceType<typeof EditMenuModal>>();

  const getMenuLabel = (record: MenuRecord) => {
    const translated = t(record.locale);
    return translated !== record.locale ? translated : record.name;
  };

  const collectExpandableKeys = (items: MenuRecord[]): number[] => {
    const keys: number[] = [];
    items.forEach((item) => {
      if (item.children?.length) {
        keys.push(item.id);
        keys.push(...collectExpandableKeys(item.children));
      }
    });
    return keys;
  };

  const isAllExpanded = computed(() => {
    const allKeys = collectExpandableKeys(tableData.value);
    return allKeys.length > 0 && allKeys.every((k) => expandedKeys.value.includes(k));
  });

  const toggleExpandAll = () => {
    if (isAllExpanded.value) {
      expandedKeys.value = [];
    } else {
      expandedKeys.value = collectExpandableKeys(tableData.value);
    }
  };

  const columns = computed(() => [
    { title: t('system.menu.columns.type'), slotName: 'type', width: 80 },
    { title: t('system.menu.columns.name'), slotName: 'name' },
    { title: t('system.menu.columns.path'), dataIndex: 'path', width: 160 },
    { title: t('system.menu.columns.permission'), slotName: 'permission', width: 200 },
    { title: t('system.menu.columns.sort'), dataIndex: 'sort', width: 60, align: 'center' as const },
    { title: t('system.menu.columns.status'), slotName: 'status', width: 80 },
    { title: t('system.menu.columns.operations'), slotName: 'action', width: 130, align: 'center' as const },
  ]);

  const fetchData = async () => {
    setLoading(true);
    try {
      const res = await queryMenuTree();
      tableData.value = res.data;
      expandedKeys.value = collectExpandableKeys(res.data);
    } finally {
      setLoading(false);
    }
  };

  const handleCreate = () => {
    editModalRef.value?.onCreate(tableData.value);
  };

  const handleCreateChild = (record: MenuRecord) => {
    editModalRef.value?.onCreateChild(record, tableData.value);
  };

  const handleEdit = (record: MenuRecord) => {
    editModalRef.value?.onEdit(record, tableData.value);
  };

  const handleDelete = (record: MenuRecord) => {
    Modal.warning({
      title: t('system.menu.delete.title'),
      content: t('system.menu.delete.content'),
      onOk: async () => {
        await deleteMenu(record.id);
        Message.success(t('system.menu.delete.success'));
        fetchData();
        appStore.fetchServerMenuConfig();
      },
    });
  };

  const onEditSuccess = () => {
    fetchData();
    appStore.fetchServerMenuConfig();
  };

  onMounted(() => {
    fetchData();
  });
</script>

<style lang="less" scoped>
  .menu-name-cell {
    display: flex;
    gap: 8px;
    align-items: center;
  }

  .menu-icon {
    flex-shrink: 0;
    color: var(--color-text-3);
    font-size: 16px;
  }

  .menu-name-text {
    font-weight: 500;
    line-height: 1.4;
  }

  .menu-name-sub {
    color: var(--color-text-3);
    font-size: 12px;
    line-height: 1.4;
  }

  .menu-code {
    padding: 2px 8px;
    font-size: 12px;
    background-color: var(--color-fill-2);
    border-radius: 2px;
  }

  .menu-component {
    color: var(--color-text-3);
    font-size: 12px;
  }

  .menu-empty {
    color: var(--color-text-4);
  }
</style>
