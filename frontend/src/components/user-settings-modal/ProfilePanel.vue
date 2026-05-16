<template>
  <div>
    <!-- Avatar centered -->
    <div class="text-center mb-6">
      <a-upload
        :file-list="[file]"
        :show-file-list="false"
        :action="uploadAction"
        :headers="{ Authorization: `Bearer ${token}` }"
        list-type="picture-card"
        @change="onChange"
        @success="onSuccess"
        @progress="onProgress"
      >
        <template #upload-button>
          <a-avatar :size="80" object-fit="cover" :style="{ cursor: 'pointer' }">
            <template #trigger-icon><icon-camera /></template>
            <img v-if="file.url" :src="file.url" />
          </a-avatar>
        </template>
      </a-upload>
    </div>

    <!-- Info fields -->
    <div class="profile-field">
      <div class="field-label">{{ t('userInfo.account.id') }}</div>
      <div class="field-value">
        <a-typography-paragraph class="!m-0" copyable>{{ userInfo.id }}</a-typography-paragraph>
      </div>
    </div>
    <div class="profile-field">
      <div class="field-label">{{ t('userInfo.account.username') }}</div>
      <div class="field-right">
        <div class="field-value">{{ userInfo.name || t('userInfo.account.username.empty') }}</div>
        <a-button type="text" size="mini" @click="openEditModal('name')">
          <icon-edit />
        </a-button>
      </div>
    </div>
    <div class="profile-field">
      <div class="field-label">{{ t('userInfo.account.nickname') }}</div>
      <div class="field-right">
        <div class="field-value">{{ userInfo.nickname || t('userInfo.account.nickname.empty') }}</div>
        <a-button type="text" size="mini" @click="openEditModal('nickname')">
          <icon-edit />
        </a-button>
      </div>
    </div>
    <div class="profile-field">
      <div class="field-label">{{ t('userInfo.account.introduction') }}</div>
      <div class="field-right">
        <div class="field-value">{{ userInfo.introduction || t('userInfo.account.introduction.empty') }}</div>
        <a-button type="text" size="mini" @click="openEditModal('introduction')">
          <icon-edit />
        </a-button>
      </div>
    </div>

    <!-- Edit Modal -->
    <a-modal
      v-model:visible="modalVisible"
      :title="modalTitle"
      title-align="start"
      simple
      :ok-loading="saving"
      @before-ok="onSave"
      @close="onReset"
    >
      <a-form ref="formRef" :model="formData" :rules="formRules" layout="vertical">
        <a-form-item :label="modalLabel" hide-label field="value" asterisk-position="end">
          <a-input
            v-if="editingField !== 'introduction'"
            v-model="formData.value"
            :placeholder="modalPlaceholder"
            :max-length="editingField === 'name' ? 20 : 20"
            show-word-limit
          >
            <template v-if="editingField === 'name'" #prefix>@</template>
          </a-input>
          <a-textarea v-else v-model="formData.value" :placeholder="modalPlaceholder" :max-length="100" show-word-limit />
        </a-form-item>
      </a-form>
    </a-modal>
  </div>
</template>

<script setup lang="ts">
  import { computed, ref, reactive } from 'vue';
  import { FileItem, FormInstance, Message } from '@arco-design/web-vue';
  import { useI18n } from 'vue-i18n';
  import { useUserStore } from '@/store';
  import { getToken } from '@/utils/auth';
  import { updateProfile } from '@/api/user';

  const { t } = useI18n();
  const userStore = useUserStore();
  const userInfo = computed(() => userStore.userInfo);

  const token = getToken();
  const uploadAction = `${import.meta.env.VITE_API_BASE_URL}/api/user/upload-avatar`;
  const file = ref<FileItem>({ uid: '-2', name: 'avatar.png', url: userInfo.value.avatar });

  const onChange = (_fileItemList: FileItem[], fileItem: FileItem) => {
    file.value.url = fileItem.url;
  };
  const onProgress = (currentFile: File) => {
    file.value = currentFile;
  };
  const onSuccess = async (response?: any) => {
    const { code, message, data } = JSON.parse(response.response);
    if (code === 0) {
      file.value.url = data.url;
      userStore.info();
      Message.success(t('userInfo.account.avatarSuccess'));
    } else {
      Message.error(message);
    }
  };

  // Edit modal
  type EditableField = 'name' | 'nickname' | 'introduction';
  const editingField = ref<EditableField>('name');
  const modalVisible = ref(false);
  const saving = ref(false);
  const formRef = ref<FormInstance>();
  const formData = reactive({ value: '' });

  const fieldConfig = computed(() => ({
    name: {
      title: t('userInfo.editInfo.username'),
      label: t('userInfo.editInfo.username'),
      placeholder: t('userInfo.editInfo.username.placeholder'),
      rules: [
        { required: true, message: t('userInfo.editInfo.username.required') },
        { pattern: /^[a-zA-Z0-9_]{1,20}$/, message: t('userInfo.editInfo.username.pattern') },
      ],
      apiKey: 'name',
    },
    nickname: {
      title: t('userInfo.editInfo.nickname'),
      label: t('userInfo.editInfo.nickname'),
      placeholder: t('userInfo.editInfo.nickname.placeholder'),
      rules: [{ required: true, message: t('userInfo.editInfo.nickname.required') }],
      apiKey: 'nickname',
    },
    introduction: {
      title: t('userInfo.editInfo.introduction'),
      label: t('userInfo.editInfo.introduction'),
      placeholder: t('userInfo.editInfo.introduction.placeholder'),
      rules: [],
      apiKey: 'introduction',
    },
  }));

  const currentConfig = computed(() => fieldConfig.value[editingField.value]);
  const modalTitle = computed(() => currentConfig.value.title);
  const modalLabel = computed(() => currentConfig.value.label);
  const modalPlaceholder = computed(() => currentConfig.value.placeholder);
  const formRules = computed(() => ({ value: currentConfig.value.rules }));

  const openEditModal = (field: EditableField) => {
    editingField.value = field;
    formData.value = (userInfo.value[field] as string) || '';
    modalVisible.value = true;
  };

  const onReset = () => {
    formRef.value?.resetFields();
  };

  const onSave = async () => {
    try {
      if (await formRef.value?.validate()) return false;
      saving.value = true;
      await updateProfile({ [currentConfig.value.apiKey]: formData.value.trim() });
      await userStore.info();
      Message.success(t('userInfo.editInfo.success'));
      return true;
    } catch {
      return false;
    } finally {
      saving.value = false;
    }
  };
</script>

<style scoped lang="less">
  .profile-field {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 14px 0;
    border-bottom: 1px solid var(--color-fill-2);
  }

  .field-label {
    flex-shrink: 0;
    color: var(--color-text-3);
    font-size: 14px;
  }

  .field-right {
    display: flex;
    flex: 1;
    gap: 4px;
    align-items: center;
    justify-content: flex-end;
    min-width: 0;
  }

  .field-value {
    overflow: hidden;
    color: var(--color-text-1);
    font-size: 14px;
    white-space: nowrap;
    text-align: right;
    text-overflow: ellipsis;
  }
</style>
