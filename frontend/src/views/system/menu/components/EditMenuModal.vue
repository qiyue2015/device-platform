<template>
  <a-drawer
    v-model:visible="visible"
    :title="isEdit ? $t('system.menu.editModal.titleEdit') : $t('system.menu.editModal.titleCreate')"
    :width="600"
    unmount-on-close
    @before-ok="onSave"
    @close="onReset"
  >
    <a-form ref="formRef" :model="formData" :rules="formRules" layout="vertical">
      <a-form-item :label="$t('system.menu.editModal.parentId')" field="parent_id">
        <a-cascader
          v-model="parentIdModel"
          :options="cascaderOptions"
          :placeholder="$t('system.menu.editModal.parentId.placeholder')"
          check-strictly
          allow-clear
        />
      </a-form-item>
      <a-form-item :label="$t('system.menu.editModal.type')" field="type">
        <a-radio-group v-model="formData.type">
          <a-radio :value="1">{{ $t('system.menu.types.directory') }}</a-radio>
          <a-radio :value="2">{{ $t('system.menu.types.menu') }}</a-radio>
          <a-radio :value="3">{{ $t('system.menu.types.button') }}</a-radio>
        </a-radio-group>
      </a-form-item>
      <a-form-item :label="$t('system.menu.editModal.name')" field="name">
        <a-input v-model="formData.name" :placeholder="$t('system.menu.editModal.name.placeholder')" />
      </a-form-item>
      <a-form-item :label="$t('system.menu.editModal.locale')" field="locale">
        <a-input v-model="formData.locale" :placeholder="$t('system.menu.editModal.locale.placeholder')" />
      </a-form-item>
      <a-form-item v-if="formData.type !== 3" :label="$t('system.menu.editModal.path')" field="path">
        <a-input v-model="formData.path" :placeholder="$t('system.menu.editModal.path.placeholder')" />
      </a-form-item>
      <a-form-item v-if="formData.type !== 3" :label="$t('system.menu.editModal.icon')" field="icon">
        <a-input v-model="formData.icon" :placeholder="$t('system.menu.editModal.icon.placeholder')" />
      </a-form-item>
      <a-form-item v-if="formData.type === 2" :label="$t('system.menu.editModal.component')" field="component">
        <a-input v-model="formData.component" :placeholder="$t('system.menu.editModal.component.placeholder')" />
      </a-form-item>
      <a-form-item v-if="formData.type === 3" :label="$t('system.menu.editModal.permission')" field="permission">
        <a-input v-model="formData.permission" :placeholder="$t('system.menu.editModal.permission.placeholder')" />
      </a-form-item>
      <a-form-item :label="$t('system.menu.editModal.sort')" field="sort">
        <a-input-number v-model="formData.sort" :min="0" :style="{ width: '100%' }" />
      </a-form-item>
      <a-form-item>
        <a-space :size="24">
          <a-checkbox v-model="formData.is_hidden">
            {{ $t('system.menu.editModal.hideInMenu') }}
          </a-checkbox>
          <a-checkbox v-model="formData.is_active">
            {{ $t('system.menu.editModal.isActive') }}
          </a-checkbox>
        </a-space>
      </a-form-item>
    </a-form>
  </a-drawer>
</template>

<script lang="ts" setup>
  import { ref, reactive, computed } from 'vue';
  import { useI18n } from 'vue-i18n';
  import { Message } from '@arco-design/web-vue';
  import type { FormInstance, CascaderOption } from '@arco-design/web-vue';
  import { useVisible } from '@/hooks';
  import { createMenu, updateMenu, type MenuRecord } from '@/api/system/menu';

  const emit = defineEmits<{ (e: 'success'): void }>();

  const { t } = useI18n();
  const { visible, setVisible } = useVisible();
  const formRef = ref<FormInstance>();
  const editingId = ref<number | null>(null);
  const isEdit = computed(() => editingId.value !== null);
  const cascaderOptions = ref<CascaderOption[]>([]);
  const formData = reactive({
    parent_id: 0 as number,
    type: 1 as number,
    name: '',
    path: '',
    component: '',
    permission: '',
    locale: '',
    icon: '',
    sort: 0,
    is_hidden: false,
    is_active: true,
  });
  const parentIdModel = computed({
    get: () => formData.parent_id || undefined,
    set: (val) => {
      formData.parent_id = (val as number) ?? 0;
    },
  });

  const formRules = {
    name: [{ required: true, message: t('system.menu.editModal.name.placeholder') }],
    locale: [{ required: true, message: t('system.menu.editModal.locale.placeholder') }],
  };

  const onReset = () => {
    editingId.value = null;
    formData.parent_id = 0;
    formData.type = 1;
    formData.name = '';
    formData.path = '';
    formData.component = '';
    formData.permission = '';
    formData.locale = '';
    formData.icon = '';
    formData.sort = 0;
    formData.is_hidden = false;
    formData.is_active = true;
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
        parent_id: formData.parent_id,
        type: formData.type,
        name: formData.name,
        path: formData.path || undefined,
        component: formData.component || undefined,
        permission: formData.permission || undefined,
        locale: formData.locale,
        icon: formData.icon || undefined,
        sort: formData.sort,
        is_hidden: formData.is_hidden,
        is_active: formData.is_active,
      };
      if (isEdit.value) {
        await updateMenu(editingId.value as number, payload);
        Message.success(t('system.menu.editModal.updateSuccess'));
      } else {
        await createMenu(payload);
        Message.success(t('system.menu.editModal.createSuccess'));
      }
      emit('success');
      done(true);
    } catch {
      done(false);
    }
  };

  // Build tree options for cascader (exclude self and descendants when editing)
  const buildCascaderOptions = (menus: MenuRecord[], excludeId?: number): CascaderOption[] => {
    return menus
      .filter((item) => item.id !== excludeId && item.type === 1)
      .map((item) => {
        const children = item.children?.length ? buildCascaderOptions(item.children, excludeId) : [];
        return {
          value: item.id,
          label: `${item.locale} (${item.name})`,
          ...(children.length ? { children } : {}),
        };
      });
  };

  const onCreate = (allMenus: MenuRecord[]) => {
    onReset();
    cascaderOptions.value = buildCascaderOptions(allMenus);
    setVisible(true);
  };

  const onCreateChild = (parent: MenuRecord, allMenus: MenuRecord[]) => {
    onReset();
    cascaderOptions.value = buildCascaderOptions(allMenus);
    formData.parent_id = parent.id;
    formData.type = parent.type === 1 ? 2 : 3;
    setVisible(true);
  };

  const onEdit = (record: MenuRecord, allMenus: MenuRecord[]) => {
    onReset();
    cascaderOptions.value = buildCascaderOptions(allMenus, record.id);
    editingId.value = record.id;
    formData.parent_id = record.parent_id || 0;
    formData.type = record.type || 1;
    formData.name = record.name;
    formData.path = record.path || '';
    formData.component = record.component || '';
    formData.permission = record.permission || '';
    formData.locale = record.locale;
    formData.icon = record.icon || '';
    formData.sort = record.sort;
    formData.is_hidden = record.is_hidden;
    formData.is_active = record.is_active;
    setVisible(true);
  };

  defineExpose({ onCreate, onCreateChild, onEdit });
</script>
