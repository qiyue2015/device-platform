<script setup lang="ts">
  import { watch, useAttrs } from 'vue';
  import { EditorContent, useEditor } from '@tiptap/vue-3';
  import StarterKit from '@tiptap/starter-kit';
  import type { AnyExtension } from '@tiptap/vue-3';
  import Placeholder from '@tiptap/extension-placeholder';
  import ControlGroup from '@/components/tiptap/control-group.vue';
  import Highlight from '@tiptap/extension-highlight';
  import Underline from '@tiptap/extension-underline';
  import Color from '@tiptap/extension-color';
  import TextAlign from '@tiptap/extension-text-align';
  import TextStyle from '@tiptap/extension-text-style';
  import { FontSize } from './extensions/FontSize';
  import Image from './extensions/Image';

  const emits = defineEmits(['update:modelValue']);

  const attrs = useAttrs();

  const props = defineProps({
    modelValue: {
      type: String,
      default: '',
    },
  });

  const tiptapEditor = useEditor({
    content: props.modelValue,
    extensions: [
      StarterKit.configure({
        heading: {
          levels: [1, 2, 3],
        },
      }) as AnyExtension,
      Placeholder.configure({
        placeholder: attrs?.placeholder as string,
      }),
      TextAlign.configure({
        types: ['heading', 'paragraph'],
      }),
      Image.configure({
        inline: true,
      }),
      Highlight,
      Underline,
      Color,
      TextStyle,
      FontSize,
    ],
    onUpdate: ({ editor }) => {
      emits('update:modelValue', editor.getHTML());
    },
  });

  watch(
    () => props.modelValue,
    (value) => {
      if (tiptapEditor.value && value !== tiptapEditor.value.getHTML()) {
        tiptapEditor.value.commands.setContent(value);
      }
    }
  );
</script>

<template>
  <div v-if="tiptapEditor" class="tiptap-editor-wrapper">
    <control-group :editor="tiptapEditor" />
    <div class="editor-content-wrapper">
      <editor-content :editor="tiptapEditor" class="editor-content" />
    </div>
  </div>
</template>

<style lang="less" scoped>
  .tiptap-editor-wrapper {
    position: relative;
    width: 100%;
    border: 1px solid var(--color-border-2);
    border-radius: var(--border-radius-small);
  }

  .editor-content-wrapper {
    position: relative;
    display: inline-flex;
    box-sizing: border-box;
    width: 100%;
    padding: 12px;
    overflow: hidden;
    color: var(--color-text-1);
    font-size: 14px;
    cursor: text;

    .editor-content {
      position: relative;
      width: 100%;
      height: 380px;
      overflow: auto;
      scrollbar-width: none;
    }

    :deep(.tiptap) {
      :first-child {
        margin-top: 0;
      }

      box-sizing: border-box;
      height: 100%;
      overflow-x: auto;
      font-family: sans-serif;
      line-height: 1.8;
      text-align: left;
      outline: none;

      p {
        margin: 1em 0;

        &.is-empty::before {
          float: left;
          height: 0;
          color: rgb(var(--gray-6));
          content: attr(data-placeholder);
          pointer-events: none;
        }
      }
    }
  }
</style>
