<script setup lang="ts">
  import { computed, PropType } from 'vue';
  import { useI18n } from 'vue-i18n';
  import type { Editor } from '@tiptap/vue-3';

  export interface FileRecord {
    id: string;
    name: string;
    path: string;
    url: string;
  }

  const { t } = useI18n();

  const props = defineProps({
    editor: {
      type: Object as PropType<Editor>,
      required: true,
    },
    toolbars: {
      type: Array as PropType<string[]>,
      default: () => [
        'undo',
        'redo',
        'divider',
        'heading1',
        'heading2',
        'heading3',
        // 'paragraph',
        'fontSize',
        'bold',
        'italic',
        'strike',
        'underline',
        'highlight',
        'bulletList',
        'orderedList',
        'blockquote',
        'alignLeft',
        'alignCenter',
        'alignRight',
        'alignJustify',
        'divider',
        'color',
        'insertImage',
        'clearFormat',
      ],
    },
  });

  // 颜色
  const textStyleColor = computed(() => {
    if (props.editor.getAttributes('textStyle').color) {
      return props.editor.getAttributes('textStyle').color;
    }
    return '#000000';
  });

  const setColor = (visible: boolean, value: string) => {
    if (!visible) {
      props.editor.chain().focus().setColor(value).run();
    }
  };

  // 字号
  const textFontSize = computed(() => {
    if (props.editor.getAttributes('textStyle').fontSize) {
      return props.editor.getAttributes('textStyle').fontSize;
    }
    return 14;
  });
  const setFontSize = (size: any) => {
    props.editor.chain().focus().setFontSize(size).run();
  };

  const onInsertImage = (images: FileRecord[]) => {
    const chain = props.editor.chain().focus();

    images.forEach((image) => {
      chain.insertContent([
        {
          type: 'image',
          attrs: {
            src: image.url,
          },
        },
      ]);
    });

    chain.run();
  };

  const allClearFormat = () => {
    props.editor.chain().focus().clearNodes().run();
    props.editor.chain().focus().unsetFontSize().run();
  };

  const toolbarMap: Record<string, any> = {
    undo: {
      label: t('common.tiptap.undo'),
      icon: 'icon-undo',
      activeCheck: () => false,
      disabled: () => !(props.editor.can().chain().focus() as any).undo?.().run?.(),
    },
    redo: {
      label: t('common.tiptap.redo'),
      icon: 'icon-redo',
      activeCheck: () => false,
      disabled: () => !(props.editor.can().chain().focus() as any).redo?.().run?.(),
    },
    heading1: {
      label: t('common.tiptap.heading1'),
      icon: 'icon-h1',
      activeCheck: () => props.editor.isActive('heading', { level: 1 }),
    },
    heading2: {
      label: t('common.tiptap.heading2'),
      icon: 'icon-h2',
      activeCheck: () => props.editor.isActive('heading', { level: 2 }),
    },
    heading3: {
      label: t('common.tiptap.heading3'),
      icon: 'icon-h3',
      activeCheck: () => props.editor.isActive('heading', { level: 3 }),
    },
    paragraph: {
      label: t('common.tiptap.paragraph'),
      icon: 'icon-sort',
      activeCheck: () => props.editor.isActive('paragraph'),
    },
    fontSize: {
      label: t('common.tiptap.fontSize'),
      icon: 'icon-font-size',
      activeCheck: () => false,
    },
    bold: {
      label: t('common.tiptap.bold'),
      icon: 'icon-bold',
      activeCheck: () => props.editor.isActive('bold'),
    },
    italic: {
      label: t('common.tiptap.italic'),
      icon: 'icon-italic',
      activeCheck: () => props.editor.isActive('italic'),
    },
    strike: {
      label: t('common.tiptap.strike'),
      icon: 'icon-strikethrough',
      activeCheck: () => props.editor.isActive('strike'),
    },
    underline: {
      label: t('common.tiptap.underline'),
      icon: 'icon-underline',
      activeCheck: () => props.editor.isActive('underline'),
    },
    highlight: {
      label: t('common.tiptap.highlight'),
      icon: 'icon-highlight',
      activeCheck: () => props.editor.isActive('highlight'),
    },
    bulletList: {
      label: t('common.tiptap.bulletList'),
      icon: 'icon-unordered-list',
      activeCheck: () => props.editor.isActive('bulletList'),
    },
    orderedList: {
      label: t('common.tiptap.orderedList'),
      icon: 'icon-ordered-list',
      activeCheck: () => props.editor.isActive('orderedList'),
    },
    blockquote: {
      label: t('common.tiptap.blockquote'),
      icon: 'icon-quote',
      activeCheck: () => props.editor.isActive('blockquote'),
    },
    alignLeft: {
      label: t('common.tiptap.alignLeft'),
      icon: 'icon-align-left',
      activeCheck: () => props.editor.isActive({ textAlign: 'left' }),
    },
    alignCenter: {
      label: t('common.tiptap.alignCenter'),
      icon: 'icon-align-center',
      activeCheck: () => props.editor.isActive({ textAlign: 'center' }),
    },
    alignRight: {
      label: t('common.tiptap.alignRight'),
      icon: 'icon-align-right',
      activeCheck: () => props.editor.isActive({ textAlign: 'right' }),
    },
    alignJustify: {
      label: t('common.tiptap.alignJustify'),
      icon: 'icon-menu',
      activeCheck: () => props.editor.isActive({ textAlign: 'justify' }),
    },
    color: { label: t('common.tiptap.color'), icon: 'icon-font-colors', activeCheck: () => false },
    insertImage: { label: t('common.tiptap.insertImage'), icon: 'icon-image', activeCheck: () => false },
    clearFormat: { label: t('common.tiptap.clearFormat'), icon: 'icon-eraser', activeCheck: () => false },
  };

  const commandMap: Record<string, () => void> = {
    undo: () => (props.editor.chain().focus() as any).undo?.().run?.(),
    redo: () => (props.editor.chain().focus() as any).redo?.().run?.(),
    heading1: () => (props.editor.chain().focus() as any).toggleHeading?.({ level: 1 }).run?.(),
    heading2: () => (props.editor.chain().focus() as any).toggleHeading?.({ level: 2 }).run?.(),
    heading3: () => (props.editor.chain().focus() as any).toggleHeading?.({ level: 3 }).run?.(),
    paragraph: () => (props.editor.chain().focus() as any).setParagraph?.().run?.(),
    bold: () => (props.editor.chain().focus() as any).toggleBold?.().run?.(),
    italic: () => (props.editor.chain().focus() as any).toggleItalic?.().run?.(),
    strike: () => (props.editor.chain().focus() as any).toggleStrike?.().run?.(),
    underline: () => props.editor.chain().focus().toggleUnderline().run(),
    highlight: () => props.editor.chain().focus().toggleHighlight().run(),
    bulletList: () => (props.editor.chain().focus() as any).toggleBulletList?.().run?.(),
    orderedList: () => (props.editor.chain().focus() as any).toggleOrderedList?.().run?.(),
    blockquote: () => (props.editor.chain().focus() as any).toggleBlockquote?.().run?.(),
    alignLeft: () => props.editor.chain().focus().setTextAlign('left').run(),
    alignCenter: () => props.editor.chain().focus().setTextAlign('center').run(),
    alignRight: () => props.editor.chain().focus().setTextAlign('right').run(),
    alignJustify: () => props.editor.chain().focus().setTextAlign('justify').run(),
    clearFormat: () => allClearFormat(),
  };

  const executeCommand = (command: string) => {
    if (commandMap[command]) commandMap[command]();
  };
</script>

<template>
  <div class="control-group">
    <a-space size="mini" wrap>
      <template v-for="option in toolbars" :key="option">
        <!-- 间隔符 -->
        <a-divider v-if="option === 'divider'" direction="vertical" />
        <!-- 按钮 -->
        <a-tooltip v-else :content="toolbarMap[option]?.label">
          <template v-if="option === 'undo' || option === 'redo'">
            <a-button
              size="small"
              :disabled="toolbarMap[option]?.disabled && toolbarMap[option]?.disabled()"
              :type="toolbarMap[option]?.activeCheck && toolbarMap[option]?.activeCheck() ? 'primary' : 'secondary'"
              @click="executeCommand(option)"
            >
              <template #icon>
                <component :is="toolbarMap[option]?.icon" />
              </template>
            </a-button>
          </template>
          <!-- 字号 -->
          <template v-else-if="option === 'fontSize'">
            <a-select :model-value="textFontSize" :options="[12, 14, 16, 18, 20]" @change="setFontSize" />
          </template>
          <!-- 颜色 -->
          <template v-else-if="option === 'color'">
            <a-color-picker :model-value="textStyleColor" show-history show-preset @popup-visible-change="setColor" />
          </template>
          <!-- 插入图片 -->
          <template v-else-if="option === 'insertImage'">
            <ImageGallery :show-file-list="false" @change="onInsertImage">
              <template #upload-button>
                <a-button size="small">
                  <template #icon>
                    <icon-image />
                  </template>
                </a-button>
              </template>
            </ImageGallery>
          </template>
          <template v-else>
            <a-button
              size="small"
              :type="toolbarMap[option]?.activeCheck && toolbarMap[option]?.activeCheck() ? 'primary' : 'secondary'"
              @click="executeCommand(option)"
            >
              <template #icon>
                <component :is="toolbarMap[option]?.icon" />
              </template>
            </a-button>
          </template>
        </a-tooltip>
      </template>
    </a-space>
  </div>
</template>

<style scoped lang="less">
  .control-group {
    display: flex;
    padding: 8px 12px 4px;
    background-color: var(--color-fill-2);
    border-bottom: 1px solid var(--color-border-2);

    :deep(.arco-color-picker) {
      width: 28px;
      height: 28px;
      padding: 7px;

      .arco-color-picker-preview {
        width: 14px;
        height: 14px;
      }
    }
  }
</style>
