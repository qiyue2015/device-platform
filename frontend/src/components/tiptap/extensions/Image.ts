import { VueNodeViewRenderer } from '@tiptap/vue-3';
import TiptapImage from '@tiptap/extension-image';
import { Component } from '@/router/routes/types';
import ImageView from '../view/ImageView.vue';

const ImageDisplay = {
  INLINE: 'inline',
  BREAK_TEXT: 'block',
  FLOAT_LEFT: 'left',
  FLOAT_RIGHT: 'right',
};

const DEFAULT_IMAGE_WIDTH = 300;

const DEFAULT_IMAGE_DISPLAY = ImageDisplay.INLINE;

const Images = TiptapImage.extend({
  addAttributes() {
    return {
      // @ts-ignore
      ...(this.parent ? this.parent() : {}),
      width: {
        default: DEFAULT_IMAGE_WIDTH,
        parseHTML: (element: HTMLElement) => {
          const width = element.style.width || element.getAttribute('width') || null;
          return width == null ? null : parseInt(width, 10);
        },
        renderHTML: (attributes: any) => {
          return {
            width: attributes.width,
          };
        },
      },
      height: {
        default: null,
        parseHTML: (element: HTMLElement) => {
          const height = element.style.height || element.getAttribute('height') || null;
          return height == null ? null : parseInt(height, 10);
        },
        renderHTML: (attributes: any) => {
          return {
            height: attributes.height,
          };
        },
      },
      display: {
        default: DEFAULT_IMAGE_DISPLAY,
        parseHTML: (element: HTMLElement) => {
          const { cssFloat, display } = element.style;
          let dp = element.getAttribute('data-display') || element.getAttribute('display');
          if (dp) {
            dp = /(inline|block|left|right)/.test(dp) ? dp : ImageDisplay.INLINE;
          } else if (cssFloat === 'left' && !display) {
            dp = ImageDisplay.FLOAT_LEFT;
          } else if (cssFloat === 'right' && !display) {
            dp = ImageDisplay.FLOAT_RIGHT;
          } else if (!cssFloat && display === 'block') {
            dp = ImageDisplay.BREAK_TEXT;
          } else {
            dp = ImageDisplay.INLINE;
          }

          return dp;
        },
        renderHTML: (attributes: any) => {
          return {
            'data-display': attributes.display,
          };
        },
      },
    };
  },

  addNodeView() {
    return VueNodeViewRenderer(ImageView as Component);
  },

  parseHTML() {
    return [
      {
        tag: 'img[src]',
      },
    ];
  },
});

export default Images;
