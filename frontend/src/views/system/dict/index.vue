<template>
  <div class="page-container">
    <a-row :gutter="16" class="flex-1 min-h-0">
      <a-col :span="8" class="h-full">
        <grid :title="$t('system.dict.type.title')">
          <GridToolbar @create="handleCreateType" @refresh="fetchTypes">
            <template #prepend>
              <a-input-search
                v-model="typeKeyword"
                :placeholder="$t('system.dict.search.placeholder')"
                style="width: 200px"
                @search="handleSearchType"
                @press-enter="handleSearchType"
              />
            </template>
          </GridToolbar>
          <GridTable
            :loading="typeLoading"
            :data="typeData"
            :columns="typeColumns"
            :pagination="typePagination"
            :row-class="(record: DictTypeRecord) => record.id === selectedTypeId ? 'row-selected' : ''"
            @row-click="handleSelectType"
            @edit="handleEditType"
            @delete="handleDeleteType"
            @page-change="onTypePageChange"
            @page-size-change="onTypePageSizeChange"
          >
            <template #status="{ record }">
              <a-badge v-if="record.is_active" status="success" :text="$t('system.dict.status.active')" />
              <a-badge v-else status="danger" :text="$t('system.dict.status.inactive')" />
            </template>
          </GridTable>
          <EditDictTypeModal ref="editTypeModalRef" @success="fetchTypes" />
        </grid>
      </a-col>
      <a-col :span="16" class="h-full">
        <grid :title="selectedType ? `${$t('system.dict.item.title')} - ${selectedType.name}` : $t('system.dict.item.title')">
          <template v-if="selectedTypeId">
            <GridToolbar @create="handleCreateItem" @refresh="fetchItems">
              <template #prepend>
                <a-input-search
                  v-model="itemKeyword"
                  :placeholder="$t('system.dict.search.itemPlaceholder')"
                  style="width: 200px"
                  @search="handleSearchItem"
                  @press-enter="handleSearchItem"
                />
              </template>
            </GridToolbar>
            <GridTable
              :loading="itemLoading"
              :data="itemData"
              :columns="itemColumns"
              :pagination="itemPagination"
              @edit="handleEditItem"
              @delete="handleDeleteItem"
              @page-change="onItemPageChange"
              @page-size-change="onItemPageSizeChange"
            >
              <template #status="{ record }">
                <a-badge v-if="record.is_active" status="success" :text="$t('system.dict.status.active')" />
                <a-badge v-else status="danger" :text="$t('system.dict.status.inactive')" />
              </template>
            </GridTable>
            <EditDictItemModal ref="editItemModalRef" :type-id="selectedTypeId" @success="fetchItems" />
          </template>
          <a-empty v-else :description="$t('system.dict.item.placeholder')" />
        </grid>
      </a-col>
    </a-row>
  </div>
</template>

<script lang="ts" setup>
  import { ref, reactive, computed, onMounted } from 'vue';
  import { useI18n } from 'vue-i18n';
  import { Modal, Message } from '@arco-design/web-vue';
  import { useLoading } from '@/hooks';
  import { HttpResponse } from '@/api/interceptor';
  import {
    queryDictTypeList,
    deleteDictType,
    queryDictItemList,
    deleteDictItem,
    type DictTypeRecord,
    type DictItemRecord,
  } from '@/api/system/dict';
  import EditDictTypeModal from './components/EditDictTypeModal.vue';
  import EditDictItemModal from './components/EditDictItemModal.vue';

  defineOptions({ name: 'SystemDict' });

  const { t } = useI18n();
  const { loading: typeLoading, setLoading: setTypeLoading } = useLoading(false);
  const { loading: itemLoading, setLoading: setItemLoading } = useLoading(false);

  const typeKeyword = ref('');
  const itemKeyword = ref('');
  const typeData = ref<DictTypeRecord[]>([]);
  const itemData = ref<DictItemRecord[]>([]);
  const selectedTypeId = ref<number>();
  const selectedType = ref<DictTypeRecord>();
  const editTypeModalRef = ref<InstanceType<typeof EditDictTypeModal>>();
  const editItemModalRef = ref<InstanceType<typeof EditDictItemModal>>();

  const typePagination = reactive({
    current: 1,
    pageSize: 20,
    total: 0,
    showTotal: true,
    showPageSize: true,
  });
  const itemPagination = reactive({
    current: 1,
    pageSize: 20,
    total: 0,
    showTotal: true,
    showPageSize: true,
  });

  const typeColumns = computed(() => [
    { title: t('system.dict.columns.name'), dataIndex: 'name' },
    { title: t('system.dict.columns.code'), dataIndex: 'code' },
    { title: t('system.dict.columns.status'), slotName: 'status', width: 80 },
    { title: t('system.dict.columns.operations'), slotName: 'action', width: 100, align: 'center' as const },
  ]);

  const itemColumns = computed(() => [
    { title: t('system.dict.columns.label'), dataIndex: 'label' },
    { title: t('system.dict.columns.value'), dataIndex: 'value' },
    { title: t('system.dict.columns.sort'), dataIndex: 'sort', width: 70, align: 'center' as const },
    { title: t('system.dict.columns.status'), slotName: 'status', width: 80 },
    { title: t('system.dict.columns.operations'), slotName: 'action', width: 100, align: 'center' as const },
  ]);

  const fetchTypes = async () => {
    setTypeLoading(true);
    try {
      const res = (await queryDictTypeList({
        name: typeKeyword.value || undefined,
        current: typePagination.current,
        pageSize: typePagination.pageSize,
      })) as unknown as HttpResponse<DictTypeRecord[]>;
      typeData.value = res.data;
      typePagination.total = res.meta?.total ?? 0;
    } finally {
      setTypeLoading(false);
    }
  };

  const fetchItems = async () => {
    if (!selectedTypeId.value) return;
    setItemLoading(true);
    try {
      const res = (await queryDictItemList({
        dictionary_type_id: String(selectedTypeId.value),
        label: itemKeyword.value || undefined,
        current: itemPagination.current,
        pageSize: itemPagination.pageSize,
      })) as unknown as HttpResponse<DictItemRecord[]>;
      itemData.value = res.data;
      itemPagination.total = res.meta?.total ?? 0;
    } finally {
      setItemLoading(false);
    }
  };

  const handleSelectType = (record: DictTypeRecord) => {
    selectedTypeId.value = record.id;
    selectedType.value = record;
    itemPagination.current = 1;
    itemKeyword.value = '';
    fetchItems();
  };

  const handleSearchType = () => {
    typePagination.current = 1;
    fetchTypes();
  };
  const handleSearchItem = () => {
    itemPagination.current = 1;
    fetchItems();
  };
  const onTypePageChange = (p: number) => {
    typePagination.current = p;
    fetchTypes();
  };
  const onTypePageSizeChange = (s: number) => {
    typePagination.pageSize = s;
    typePagination.current = 1;
    fetchTypes();
  };
  const onItemPageChange = (p: number) => {
    itemPagination.current = p;
    fetchItems();
  };
  const onItemPageSizeChange = (s: number) => {
    itemPagination.pageSize = s;
    itemPagination.current = 1;
    fetchItems();
  };

  const handleCreateType = () => {
    editTypeModalRef.value?.onCreate();
  };
  const handleEditType = (record: DictTypeRecord) => {
    editTypeModalRef.value?.onEdit(record);
  };
  const handleDeleteType = (record: DictTypeRecord) => {
    Modal.warning({
      title: t('system.dict.delete.title'),
      content: t('system.dict.delete.content'),
      onOk: async () => {
        await deleteDictType(record.id);
        Message.success(t('system.dict.delete.success'));
        if (selectedTypeId.value === record.id) {
          selectedTypeId.value = undefined;
          selectedType.value = undefined;
          itemData.value = [];
        }
        fetchTypes();
      },
    });
  };

  const handleCreateItem = () => {
    editItemModalRef.value?.onCreate();
  };
  const handleEditItem = (record: DictItemRecord) => {
    editItemModalRef.value?.onEdit(record);
  };
  const handleDeleteItem = (record: DictItemRecord) => {
    Modal.warning({
      title: t('system.dict.deleteItem.title'),
      content: t('system.dict.deleteItem.content'),
      onOk: async () => {
        await deleteDictItem(record.id);
        Message.success(t('system.dict.deleteItem.success'));
        fetchItems();
      },
    });
  };

  onMounted(() => {
    fetchTypes();
  });
</script>

<style lang="less" scoped>
  :deep(.row-selected) td {
    background-color: var(--color-primary-light-1) !important;
  }
</style>
