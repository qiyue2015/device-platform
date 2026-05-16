<template>
  <a-drawer
    v-model:visible="visible"
    :title="isEdit ? $t('system.role.editModal.titleEdit') : $t('system.role.editModal.titleCreate')"
    :width="600"
    :ok-button-props="({ style: { display: readonly ? 'none' : undefined } } as any)"
    unmount-on-close
    @before-ok="onSave"
    @close="onReset"
  >
    <a-form ref="formRef" :model="formData" :rules="formRules" :disabled="readonly" layout="vertical">
      <a-form-item :label="$t('system.role.editModal.name')" field="name">
        <a-input v-model="formData.name" :placeholder="$t('system.role.editModal.name.placeholder')" />
      </a-form-item>
      <a-form-item :label="$t('system.role.editModal.locale')" field="locale">
        <a-input v-model="formData.locale" :placeholder="$t('system.role.editModal.locale.placeholder')" />
      </a-form-item>
      <a-form-item field="menu_ids">
        <div class="permission-groups">
          <div class="permission-toolbar">
            <span class="permission-toolbar-label">{{ $t('system.role.editModal.permissions') }}</span>
            <div class="permission-toolbar-actions">
              <a-checkbox
                :model-value="isAllChecked"
                :indeterminate="isAllIndeterminate"
                @change="(val: boolean) => onSelectAll(val)"
              >
                {{ $t('system.role.editModal.selectAll') }}
              </a-checkbox>
              <a-link :hoverable="false" @click="toggleAllGroups">
                {{ isAllExpanded ? $t('system.role.editModal.collapseAll') : $t('system.role.editModal.expandAll') }}
              </a-link>
            </div>
          </div>
          <div v-for="group in allMenus" :key="group.id" class="permission-group">
            <div class="permission-group-header" @click="toggleGroup(group.id)">
              <a-checkbox
                :model-value="isGroupChecked(group)"
                :indeterminate="isGroupIndeterminate(group)"
                @click.stop
                @change="(val: boolean) => onGroupChange(group, val)"
              >
                {{ group.title }}
              </a-checkbox>
              <icon-right :class="['permission-group-arrow', { expanded: expandedGroups.has(group.id) }]" />
            </div>
            <div v-show="expandedGroups.has(group.id)" class="permission-group-body">
              <template v-for="child in group.children" :key="child.id">
                <!-- 页面(type=2): 直接渲染行 + 按钮 -->
                <div v-if="child.type === 2" class="permission-section">
                  <div class="permission-row">
                    <div class="permission-row-label">
                      <a-checkbox
                        :model-value="isGroupChecked(child)"
                        :indeterminate="isGroupIndeterminate(child)"
                        @change="(val: boolean) => onGroupChange(child, val)"
                      >
                        {{ child.title }}
                      </a-checkbox>
                    </div>
                    <div v-if="child.children?.length" class="permission-row-items">
                      <a-checkbox
                        v-for="item in child.children"
                        :key="item.id"
                        :model-value="formData.menu_ids.includes(item.id)"
                        class="permission-btn-item"
                        @change="(val: boolean) => onItemChange(item.id, val)"
                      >
                        <span>{{ item.title }}</span>
                      </a-checkbox>
                    </div>
                  </div>
                </div>
                <!-- 目录(type=1): 展平显示，目录行 + 子页面行同色 -->
                <div v-if="child.type === 1" class="permission-section">
                  <div class="permission-row">
                    <div class="permission-row-label">
                      <a-checkbox
                        :model-value="isGroupChecked(child)"
                        :indeterminate="isGroupIndeterminate(child)"
                        @change="(val: boolean) => onGroupChange(child, val)"
                      >
                        {{ child.title }}
                      </a-checkbox>
                    </div>
                    <div class="permission-row-items" />
                  </div>
                  <div
                    v-for="subPage in child.children?.filter((c: any) => c.type === 2)"
                    :key="subPage.id"
                    class="permission-row"
                  >
                    <div class="permission-row-label">
                      <a-checkbox
                        :model-value="isGroupChecked(subPage)"
                        :indeterminate="isGroupIndeterminate(subPage)"
                        @change="(val: boolean) => onGroupChange(subPage, val)"
                      >
                        {{ subPage.title }}
                      </a-checkbox>
                    </div>
                    <div v-if="subPage.children?.length" class="permission-row-items">
                      <a-checkbox
                        v-for="item in subPage.children"
                        :key="item.id"
                        :model-value="formData.menu_ids.includes(item.id)"
                        class="permission-btn-item"
                        @change="(val: boolean) => onItemChange(item.id, val)"
                      >
                        <span>{{ item.title }}</span>
                      </a-checkbox>
                    </div>
                  </div>
                </div>
              </template>
            </div>
          </div>
        </div>
      </a-form-item>
    </a-form>
  </a-drawer>
</template>

<script lang="ts" setup>
  import { ref, reactive, computed } from 'vue';
  import { useI18n } from 'vue-i18n';
  import { Message } from '@arco-design/web-vue';
  import type { FormInstance } from '@arco-design/web-vue';
  import { useVisible } from '@/hooks';
  import { createRole, updateRole, type RoleRecord } from '@/api/system/role';
  import type { MenuRecord } from '@/api/system/menu';

  const emit = defineEmits<{ (e: 'success'): void }>();

  const { t } = useI18n();
  const { visible, setVisible } = useVisible();
  const formRef = ref<FormInstance>();
  const editingId = ref<number | null>(null);
  const readonly = ref(false);
  const isEdit = computed(() => editingId.value !== null);
  const allMenus = ref<any[]>([]);
  const expandedGroups = ref<Set<number>>(new Set());

  const formData = reactive({
    name: '',
    locale: '',
    menu_ids: [] as (number | string)[],
  });

  const formRules = {
    name: [{ required: true, message: t('system.role.editModal.name.placeholder') }],
  };

  const expandAllGroups = () => {
    expandedGroups.value = new Set(allMenus.value.map((g) => g.id));
  };

  const isAllExpanded = computed(
    () => allMenus.value.length > 0 && allMenus.value.every((g) => expandedGroups.value.has(g.id))
  );

  const toggleAllGroups = () => {
    if (isAllExpanded.value) {
      expandedGroups.value.clear();
    } else {
      expandAllGroups();
    }
  };

  const toggleGroup = (id: number) => {
    if (expandedGroups.value.has(id)) {
      expandedGroups.value.delete(id);
    } else {
      expandedGroups.value.add(id);
    }
  };

  const onReset = () => {
    editingId.value = null;
    readonly.value = false;
    formData.name = '';
    formData.locale = '';
    formData.menu_ids = [];
    expandedGroups.value.clear();
    formRef.value?.resetFields();
  };

  const onSave = async (done: (closed: boolean) => void) => {
    try {
      const valid = await formRef.value?.validate();
      if (valid) {
        done(false);
        return;
      }
      const payload = {
        name: formData.name,
        locale: formData.locale || undefined,
        menu_ids: formData.menu_ids.map(Number),
      };
      if (isEdit.value) {
        await updateRole(editingId.value as number, payload);
        Message.success(t('system.role.editModal.updateSuccess'));
      } else {
        await createRole(payload);
        Message.success(t('system.role.editModal.createSuccess'));
      }
      emit('success');
      done(true);
    } catch {
      done(false);
    }
  };

  // Convert MenuRecord to Tree data adding title (localized) and type
  const formatTreeData = (menus: MenuRecord[]): any[] => {
    return menus.map((m) => ({
      id: m.id,
      type: m.type,
      title: t(m.locale) || m.name,
      children: m.children?.length ? formatTreeData(m.children) : undefined,
    }));
  };

  const onCreate = (menus: MenuRecord[]) => {
    onReset();
    allMenus.value = formatTreeData(menus);
    expandAllGroups();
    setVisible(true);
  };

  const onEdit = (record: RoleRecord, menus: MenuRecord[]) => {
    onReset();
    allMenus.value = formatTreeData(menus);
    expandAllGroups();
    editingId.value = record.id;
    readonly.value = record.name === 'super-admin';
    formData.name = record.name;
    formData.locale = record.locale || '';
    formData.menu_ids = record.menus?.map((m) => m.id) || [];
    setVisible(true);
  };

  // Collect all descendant IDs of a node
  const collectIds = (node: any): number[] => {
    const ids = [node.id];
    if (node.children) {
      node.children.forEach((c: any) => ids.push(...collectIds(c)));
    }
    return ids;
  };

  // All menus select all / deselect all
  const allIds = computed(() => allMenus.value.flatMap(collectIds));
  const isAllChecked = computed(() => allIds.value.length > 0 && allIds.value.every((id) => formData.menu_ids.includes(id)));
  const isAllIndeterminate = computed(() => {
    const checked = allIds.value.filter((id) => formData.menu_ids.includes(id));
    return checked.length > 0 && checked.length < allIds.value.length;
  });
  const onSelectAll = (checked: boolean) => {
    if (checked) {
      formData.menu_ids = [...allIds.value];
    } else {
      formData.menu_ids = [];
    }
  };

  const isGroupChecked = (group: any): boolean => {
    const ids = collectIds(group);
    return ids.every((id) => formData.menu_ids.includes(id));
  };

  const isGroupIndeterminate = (group: any): boolean => {
    const ids = collectIds(group);
    const checked = ids.filter((id) => formData.menu_ids.includes(id));
    return checked.length > 0 && checked.length < ids.length;
  };

  // Sync non-leaf node IDs: add if any descendant checked, remove if none
  const syncAncestorIds = () => {
    const sync = (node: any): boolean => {
      if (!node.children?.length) {
        return formData.menu_ids.includes(node.id);
      }
      const hasChecked = node.children.map((c: any) => sync(c)).some(Boolean);
      const idx = formData.menu_ids.indexOf(node.id);
      if (hasChecked && idx === -1) {
        formData.menu_ids.push(node.id);
      } else if (!hasChecked && idx !== -1) {
        formData.menu_ids.splice(idx, 1);
      }
      return hasChecked;
    };
    allMenus.value.forEach(sync);
  };

  const onGroupChange = (group: any, checked: boolean) => {
    const ids = collectIds(group);
    if (checked) {
      const set = new Set(formData.menu_ids);
      ids.forEach((id) => set.add(id));
      formData.menu_ids = [...set];
    } else {
      formData.menu_ids = formData.menu_ids.filter((id) => !ids.includes(id as number));
    }
    syncAncestorIds();
  };

  const onItemChange = (id: number, checked: boolean) => {
    if (checked) {
      formData.menu_ids.push(id);
    } else {
      formData.menu_ids = formData.menu_ids.filter((v) => v !== id);
    }
    syncAncestorIds();
  };

  defineExpose({ onCreate, onEdit });
</script>

<style scoped lang="less">
  .permission-groups {
    width: 100%;
  }

  .permission-toolbar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 12px;
  }

  .permission-toolbar-label {
    font-weight: 500;
  }

  .permission-toolbar-actions {
    display: flex;
    gap: 12px;
    align-items: center;
  }

  .permission-group {
    margin-bottom: 12px;
    overflow: hidden;
    border: 1px solid var(--color-border-2);
    border-radius: 6px;
  }

  .permission-group-header {
    display: flex;
    gap: 8px;
    align-items: center;
    padding: 10px 12px;
    font-weight: 500;
    background-color: var(--color-fill-2);
    border-bottom: 1px solid var(--color-border-2);
    cursor: pointer;
    user-select: none;
  }

  .permission-group-arrow {
    margin-left: auto;
    color: var(--color-text-3);
    font-size: 12px;
    transition: transform 0.2s;

    &.expanded {
      transform: rotate(90deg);
    }
  }

  .permission-group-body {
    padding: 0;
  }

  .permission-section {
    border-bottom: 1px solid var(--color-border-2);

    &:last-child {
      border-bottom: none;
    }
  }

  .permission-row {
    display: flex;
    align-items: flex-start;
    transition: background-color 0.1s;

    &:hover {
      background-color: var(--color-fill-1);
    }

    & + .permission-row {
      border-top: 1px solid var(--color-border-1);
    }
  }

  .permission-row-label {
    display: flex;
    gap: 6px;
    align-items: center;
    width: 160px;
    min-width: 160px;
    padding: 10px 12px;
    border-right: 1px solid var(--color-border-1);
  }

  .permission-row-items {
    display: grid;
    flex: 1;
    grid-template-columns: repeat(4, 1fr);
    gap: 8px;
    padding: 10px 12px;
  }

  .permission-btn-item {
    display: inline-flex;
    align-items: center;
  }
</style>
