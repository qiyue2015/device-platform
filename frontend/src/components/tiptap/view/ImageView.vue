<script setup lang="ts">
  /**
   * https://github.com/htmlxudong/vue3-tiptap/blob/master/src/components/component/extensions/ImageView.vue
   */
  import { computed, reactive } from 'vue';
  import { NodeViewWrapper, nodeViewProps } from '@tiptap/vue-3';
  import { resolveImg } from '@/utils';

  interface ImageResult {
    complete: boolean;
    width: number;
    height: number;
    src: string;
  }

  const props = defineProps(nodeViewProps);

  const MIN_SIZE = 20;
  const MAX_SIZE = props.editor.view.dom?.clientWidth;
  const originalSize = reactive({ width: 0, height: 0 });

  const displayCollection = reactive<string[]>(['inline', 'block', 'left', 'right']);

  const src = computed(() => props.node.attrs.src);
  const width = computed(() => props.node.attrs.width);
  const height = computed(() => props.node.attrs.height);
  const display = computed(() => props.node.attrs.display);

  const imageViewClass = computed(() => ['image-view', `image-view--${display.value}`]);

  const loadImage = async () => {
    const result = (await resolveImg(src.value)) as ImageResult;

    if (!result.complete) {
      result.width = MIN_SIZE;
      result.height = MIN_SIZE * 0.75;
    }

    originalSize.width = result.width;
    originalSize.height = result.height;
  };

  loadImage();

  const selectImage = () => {
    const pos = typeof props.getPos === 'function' ? props.getPos() : 0;
    props.editor?.commands.setNodeSelection(pos ?? 0);
  };

  const onSliderChange = (value: any) => {
    const scale = originalSize.height / originalSize.width;

    props.updateAttributes?.({ width: value, height: scale * value });
  };
</script>

<template>
  <node-view-wrapper as="span" :class="imageViewClass">
    <div class="image-view__body">
      <a-popover class="tools-wrapper" :position="display !== 'right' ? 'tl' : 'tr'" trigger="click">
        <template #content>
          <a-space size="mini" fill>
            <a-button
              v-for="item in displayCollection"
              :key="item"
              type="text"
              size="small"
              @click="updateAttributes({ display: item })"
            >
              {{ item }}
            </a-button>
            <a-divider direction="vertical" />
            <a-slider
              :default-value="300"
              :min="MIN_SIZE"
              :max="MAX_SIZE"
              :step="10"
              style="width: 120px"
              @change="onSliderChange"
            />
          </a-space>
        </template>
        <img :src="src" :alt="node.attrs.alt" :width="width" :height="height" @click="selectImage" />
      </a-popover>
    </div>
  </node-view-wrapper>
</template>

<style lang="less">
  .arco-popover-content {
    margin-top: 0;
  }

  .arco-popover-popup-content {
    padding: 6px 12px 6px 6px;
  }
</style>

<style scoped lang="less">
  .image-view {
    display: inline-block;
    float: none;
    max-width: 100%;
    margin: 0;
    line-height: 0;
    vertical-align: baseline;
    user-select: none;

    &--inline {
      display: inline-block;
    }

    &--block {
      display: block;
    }

    &--left {
      float: left;
      margin-right: 12px;
      margin-left: 0;
    }

    &--right {
      float: right;
      margin-right: 0;
      margin-left: 12px;
    }

    &__body {
      position: relative;
      display: inline-block;
      clear: both;
      max-width: 100%;
      font-size: 0;
      cursor: pointer;
      transition: all 0.2s ease-in;
    }
  }
</style>
