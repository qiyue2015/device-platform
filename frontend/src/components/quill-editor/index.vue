<template>
  <div class="editor-wrapper">
    <div ref="quillEditor" />
    <!--    <image-box v-model:visible="visible" multiple @confirm="onConfirm" />-->
  </div>
</template>

<script lang="ts" setup>
  import { onMounted, onUnmounted, ref, watch } from 'vue';
  import 'quill/dist/quill.snow.css';
  import Quill from 'quill';

  const props = defineProps<{ modelValue: string }>();
  const emits = defineEmits(['update:modelValue']);
  const quillEditor = ref<HTMLElement | null>(null);
  const visible = ref(false);

  let quill: Quill | null = null;
  let isInitialLoad = true; // Flag to determine the initial load

  const defaultOptions = {
    theme: 'snow',
    boundary: document.body,
    modules: {
      toolbar: {
        container: [
          // [{ header: [1, 2, 3, 4, 5, 6, false] }],
          ['bold', 'italic', 'underline', 'strike'],
          [{ align: [] }, { indent: '-1' }, { indent: '+1' }],
          [{ list: 'ordered' }, { list: 'bullet' }],
          [{ color: [] }, { background: [] }],
          ['link', 'image', 'video'],
          ['clean'],
        ],
        handlers: {
          image: () => {
            visible.value = true;
          },
        },
      },
    },
    placeholder: 'Insert content here ...',
    readOnly: false,
  };

  // const onConfirm = (data: any) => {
  //   if (quill) {
  //     const range = quill.getSelection(true);
  //     forEach(data, (item) => {
  //       quill?.insertEmbed(range.index, 'image', item.url);
  //     });
  //   }
  // };

  onMounted(() => {
    if (quillEditor.value) {
      quill = new Quill(quillEditor.value, defaultOptions);

      // Set initial content without triggering 'text-change'
      quill.root.innerHTML = props.modelValue || '';

      quill.on('text-change', () => {
        if (!isInitialLoad) {
          const htmlValue = quill?.root.innerHTML;
          emits('update:modelValue', htmlValue);
        }
        isInitialLoad = false; // Reset flag after the first change
      });
    }
  });

  // Watch for external changes to modelValue and update the editor content without logging
  watch(
    () => props.modelValue,
    (newVal) => {
      if (quill && quill.root.innerHTML !== newVal) {
        // Apply changes without triggering 'text-change'
        quill.root.innerHTML = newVal || '';
      }
    },
    { immediate: false }
  );

  onUnmounted(() => {
    if (quill) {
      quill.off('text-change');
      quill = null;
    }
  });
</script>

<style lang="less" scoped>
  .editor-wrapper {
    display: block;
    width: 100%;

    :deep(.ql-toolbar.ql-snow),
    :deep(.ql-container) {
      border: 1px solid var(--color-neutral-3);
      border-radius: var(--border-radius-small);
    }

    :deep(.ql-editor) {
      min-height: 180px;
    }
  }
</style>
