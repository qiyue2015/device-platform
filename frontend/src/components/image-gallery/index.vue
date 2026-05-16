<script setup lang="ts">
  import { computed, onMounted, ref, useAttrs, useSlots, watch } from 'vue';
  import { Message, FileItem } from '@arco-design/web-vue';
  import { useI18n } from 'vue-i18n';
  import { getToken } from '@/utils/auth';
  import { FileRecord, deleteFiles, queryFiles } from '@/api/file';

  const { t } = useI18n();

  const props = defineProps({
    modelValue: {
      type: [Array, String],
      default: () => [],
    },
    buttonText: {
      type: String,
      default: '',
    },
    name: {
      type: String,
      default: 'file',
    },
  });

  const emits = defineEmits(['update:modelValue', 'change']);

  const attrs = useAttrs();
  const slots = useSlots();

  const token = getToken();
  const action = `${import.meta.env.VITE_API_BASE_URL}/api/upload/image`;

  const fileLimit = ref(attrs.limit || 0);
  const fileList = ref<FileItem[]>([]);
  const selectKeys = ref<string[]>([]);
  const selectCount = computed(() => selectKeys.value.length);

  const show = ref(false);
  const loading = ref(false);
  const current = ref(1);
  const pageSize = ref(24);
  const total = ref(0);
  const list = ref<FileRecord[]>([]);
  const isEmptyData = computed(() => list.value.length === 0 && !loading.value);

  const deleteLoading = ref(false);
  const uploadFiles = ref<string[]>([]);
  const uploadLoading = computed(() => uploadFiles.value.length > 0);

  const fetchData = async () => {
    try {
      loading.value = true;
      const { data, meta } = await queryFiles({
        current: current.value,
        pageSize: pageSize.value,
      });
      list.value = data;
      total.value = meta.total;
      pageSize.value = meta.page_size;
    } finally {
      loading.value = false;
    }
  };

  const onRefresh = () => {
    current.value = 1;
    fetchData();
  };

  const onChangePage = (page: number) => {
    current.value = page;
    fetchData();
  };

  const onDeleteItems = async () => {
    try {
      deleteLoading.value = true;
      await deleteFiles(selectKeys.value);
      selectKeys.value = [];
      onRefresh();
    } finally {
      deleteLoading.value = false;
    }
  };

  // 仅用于触发弹窗，它不做其它操作
  const handleButtonClick = () => {
    return new Promise<FileList>((resolve) => {
      show.value = true;

      onRefresh();

      // 返回一个空的 FileList 以阻止后续的上传动作
      resolve(new DataTransfer().files);
    });
  };

  // 取消弹窗
  const handleCancelModal = () => {
    show.value = false;
    selectKeys.value = [];
  };

  // 确认弹窗，返回图片给父组件
  const handleConfirmModal = () => {
    show.value = false;

    // 全部返回
    const filterFiles = list.value.filter((item) => selectKeys.value.includes(item.id));
    const newFiles = filterFiles.map((item) => ({ uid: item.id, name: item.name, url: item.url }));
    const allFiles = [...fileList.value, ...newFiles];

    if (fileLimit.value === 1) {
      // 返回最后 1 张选中的图片
      const file = allFiles[allFiles.length - 1];
      fileList.value = [file]; // 为了更改视图
      emits('update:modelValue', file?.url || '');
      emits('change', fileList.value);
    } else if (fileLimit.value > 1) {
      // 返回张选中的图片里最后的 fileLimit.value 张
      const files = allFiles.slice(-fileLimit.value);
      fileList.value = files; // 为了更改视图
      emits('update:modelValue', files);
      emits('change', files);
    } else {
      // 全部返回
      fileList.value = allFiles; // 为了更改视图
      emits('update:modelValue', allFiles);
      emits('change', allFiles);
    }
  };

  // 关闭弹窗
  const closeModal = () => {
    selectKeys.value = [];
  };

  // 移除图片
  const beforeRemove = (fileItem: FileItem) => {
    return new Promise<boolean>((resolve) => {
      if (fileLimit.value === 1) {
        emits('update:modelValue', '');
      } else {
        const newFiles = fileList.value.filter((item) => item.uid !== fileItem.uid);
        emits('update:modelValue', newFiles);
      }
      resolve(true);
    });
  };

  // 上传前
  const beforeUpload = (file: File) => {
    return new Promise<File>((resolve) => {
      uploadFiles.value.push(file.name);
      resolve(file);
    });
  };

  // 上传成功
  const uploadSuccess = (response?: any) => {
    const { code, message } = response.response;

    // 移除上传中的文件
    uploadFiles.value = uploadFiles.value.filter((item) => item !== response.file.name);

    if (code !== 0) {
      Message.error(message);
    }
  };

  // 上传失败
  const uploadError = (error: FileItem) => {
    uploadFiles.value = uploadFiles.value.filter((item) => item !== error.name);
    Message.error(error?.response?.message || t('common.message.uploadFailed'));
  };

  // 监控待上传文件列表，当全部上传完成时，刷新列表
  watch(uploadFiles, (files: string[]) => {
    if (files.length === 0) {
      onRefresh();
    }
  });

  onMounted(() => {
    // 构造 fileList 的目的是为了给组件展示图片例表使用 <a-uploader />
    if (Array.isArray(props.modelValue)) {
      fileList.value = props.modelValue.map((item, index) => {
        return {
          uid: item.id || `uid-${index}-${Date.now()}`,
          name: item.url.substring(item.url.lastIndexOf('/') + 1),
          url: item.url,
        };
      });
    } else if (props.modelValue) {
      // 传入字符串时，表示只能 1 张图
      fileLimit.value = 1;
      fileList.value = [
        {
          uid: `uid-${Date.now()}`,
          name: props.modelValue.substring(props.modelValue.lastIndexOf('/') + 1),
          url: props.modelValue,
        },
      ];
    } else {
      fileList.value = [];
    }
  });
</script>

<template>
  <div class="uploader-container">
    <!--主要用于展示图片信息及上传按钮，并不太想去重写样式，就复用它了-->
    <!--还好它有个 button-click 可以阻止后续的动作-->
    <a-upload
      v-bind="{ ...attrs }"
      v-model:file-list="fileList"
      image-loading="lazy"
      image-preview
      @before-remove="beforeRemove"
      @button-click="handleButtonClick"
    >
      <template v-for="(slotContent, slotName) in slots" #[slotName]>
        <slot :name="slotName" v-bind="slotContent" />
      </template>
      <template v-if="!slots['upload-button'] && attrs['list-type'] !== 'picture-card'" #upload-button>
        <a-button :loading="uploadLoading" type="primary">
          <template #icon><icon-upload /></template>
          {{ buttonText || t('common.imageGallery.selectImage') }}
        </a-button>
      </template>
    </a-upload>

    <!--图片上传、选择弹窗-->
    <a-modal v-model:visible="show" :mask-closable="false" width="810px" title-align="start" @close="closeModal">
      <template #title> {{ t('common.imageGallery.title') }} </template>
      <a-space direction="vertical" size="medium" fill>
        <div class="toolbar-container">
          <a-space>
            <!--这个按钮才实现了上传功能-->
            <a-upload
              :multiple="true"
              :show-file-list="false"
              :action="action"
              :headers="{ Authorization: `Bearer ${token}` }"
              name="image"
              accept="image/png, image/jpeg, image/gif"
              @success="uploadSuccess"
              @error="uploadError"
              @before-upload="beforeUpload"
            >
              <template #upload-button>
                <a-button :loading="uploadLoading" type="primary">
                  <template #icon><icon-upload /></template>
                  {{ t('common.imageGallery.uploadImage') }}
                </a-button>
              </template>
            </a-upload>
            <a-button v-if="selectCount" :loading="deleteLoading" type="primary" status="danger" @click="onDeleteItems">
              {{ t('common.imageGallery.deleteCount', { count: selectCount }) }}
            </a-button>
          </a-space>
          <a-button @click="onRefresh">
            <template #icon><icon-refresh /></template>
          </a-button>
        </div>
        <a-spin :loading="loading" :class="{ 'gallery-container-empty': isEmptyData }" class="gallery-container">
          <a-empty v-if="isEmptyData" />
          <a-checkbox-group v-model="selectKeys" type="button">
            <div class="gallery-image-container">
              <a-checkbox v-for="item in list" :key="item.id" :value="item.id">
                <template #checkbox>
                  <a-image :src="item.url" :preview="false" width="120" height="90" fit="cover" show-loader />
                </template>
              </a-checkbox>
            </div>
          </a-checkbox-group>
        </a-spin>
      </a-space>
      <template #footer>
        <div class="footer-container">
          <a-pagination :total="total" :current="current" :page-size="pageSize" show-total @change="onChangePage" />
          <div class="footer-button">
            <a-button @click="handleCancelModal">{{ t('common.action.cancel') }}</a-button>
            <a-button type="primary" @click="handleConfirmModal">{{ t('common.action.confirm') }}</a-button>
          </div>
        </div>
      </template>
    </a-modal>
  </div>
</template>

<style scoped lang="less">
  .toolbar-container,
  .footer-container {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }

  .gallery-container {
    display: flex;
    min-height: 390px;

    &-empty {
      align-items: center;
    }

    .gallery-image-container {
      display: flex;
      flex-wrap: wrap;
      gap: 10px;
      align-content: flex-start;
      width: 100%;
      min-height: 390px;

      .arco-radio,
      .arco-checkbox {
        margin-right: 0;
        padding-left: 0;
      }

      .arco-image {
        overflow: hidden;
        border: 2px solid var(--color-neutral-1);
      }

      .arco-checkbox-checked {
        .arco-image {
          border-color: rgb(var(--primary-6));
        }
      }
    }
  }

  .footer-container {
    .footer-button {
      display: flex;
      gap: 12px;
      align-items: center;
    }
  }

  :deep(.arco-upload-list-picture) {
    img {
      width: 100%;
      height: 100%;
      object-fit: cover;
    }
  }
</style>
