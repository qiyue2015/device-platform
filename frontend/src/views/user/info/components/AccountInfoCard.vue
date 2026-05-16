<template>
  <a-card v-bind="{ ...attrs }">
    <div class="flex flex-col md:flex-row items-center gap-4">
      <div class="w-32 text-center">
        <a-upload
          :file-list="fileList"
          :show-file-list="false"
          :custom-request="customUpload"
          list-type="picture-card"
          accept="image/png,image/jpeg,image/jpg,image/gif"
          :limit="1"
          @change="onChange"
        >
          <template #upload-button>
            <a-avatar :size="84" class="info-avatar" object-fit="cover">
              <template #trigger-icon>
                <icon-camera />
              </template>
              <img v-if="avatarUrl" :src="avatarUrl" />
            </a-avatar>
          </template>
        </a-upload>
      </div>
      <div class="flex-1">
        <a-descriptions :column="1" class="pt-[8px]">
          <a-descriptions-item :label="t('userInfo.account.nickname')">
            {{ userInfo.nickname || t('userInfo.account.nickname.empty') }}
          </a-descriptions-item>
          <a-descriptions-item :label="t('userInfo.account.introduction')">
            {{ userInfo.introduction || t('userInfo.account.introduction.empty') }}
          </a-descriptions-item>
          <a-descriptions-item :label="t('userInfo.account.id')">
            <a-typography-paragraph class="!m-0" copyable> {{ userInfo.id }} </a-typography-paragraph>
          </a-descriptions-item>
        </a-descriptions>
      </div>
      <a-button type="outline" @click="onEditAccountInfo">{{ t('userInfo.account.editProfile') }}</a-button>
    </div>

    <EditAccountInfoModal ref="EditAccountInfoModalRef" />
  </a-card>
</template>

<script lang="ts" setup>
  import { computed, ref, useAttrs } from 'vue';
  import { FileItem, Message, RequestOption } from '@arco-design/web-vue';
  import { useI18n } from 'vue-i18n';
  import { useUserStore } from '@/store';
  import EditAccountInfoModal from './EditAccountInfoModal.vue';

  const { t } = useI18n();
  const attrs = useAttrs();

  const userStore = useUserStore();
  const userInfo = computed(() => userStore.userInfo);
  const avatarUrl = ref(userInfo.value.avatar);
  const fileList = ref<FileItem[]>([]);

  const customUpload = async (option: RequestOption) => {
    const { fileItem, onError, onSuccess } = option;

    try {
      // Validate file size (5MB limit)
      const maxSize = 5 * 1024 * 1024;
      if (fileItem.file && fileItem.file.size > maxSize) {
        Message.error(t('userInfo.account.avatarSizeError'));
        onError();
        return;
      }

      // Upload avatar
      const res = await userStore.updateAvatar(fileItem.file as File);
      avatarUrl.value = res.data.avatar_url;
      Message.success(t('userInfo.account.avatarSuccess'));
      onSuccess();
    } catch (error: any) {
      Message.error(error.message || t('userInfo.account.avatarError'));
      onError();
    }
  };

  const onChange = (fileItemList: FileItem[]) => {
    fileList.value = fileItemList;
  };

  // 修改昵称
  const EditAccountInfoModalRef = ref<InstanceType<typeof EditAccountInfoModal>>();
  const onEditAccountInfo = () => {
    EditAccountInfoModalRef.value?.onEdit();
  };
</script>
