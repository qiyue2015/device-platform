import { App } from 'vue';
import Breadcrumb from './breadcrumb/index.vue';
import Grid from './grid/index.vue';
import GridTable from './grid/grid-table.vue';
import GridToolbar from './grid/grid-toolbar.vue';
import ImageGallery from './image-gallery/index.vue';
import QuillEditor from './quill-editor/index.vue';
import Tiptap from './tiptap/index.vue';
import QQMapSelect from './qq-map-select/index.vue';

export default {
  install(Vue: App) {
    Vue.component('Breadcrumb', Breadcrumb);
    Vue.component('Grid', Grid);
    Vue.component('GridToolbar', GridToolbar);
    Vue.component('GridTable', GridTable);
    Vue.component('ImageGallery', ImageGallery);
    Vue.component('QuillEditor', QuillEditor);
    Vue.component('Tiptap', Tiptap);
    Vue.component('QQMapSelect', QQMapSelect);
  },
};
