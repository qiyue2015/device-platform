import notEmpty from '../utils.mjs';

export default {
  description: 'Generate vue view',
  prompts: [
    {
      type: 'input',
      name: 'name',
      message: '请输入 view 英文名称',
      validate: notEmpty('name'),
    },
    {
      type: 'input',
      name: 'zh_name',
      message: '请输入 view 中文名称',
      validate: notEmpty('zh_name'),
    },
    {
      type: 'checkbox',
      name: 'blocks',
      message: 'Blocks:',
      choices: [
        {
          name: '<template>',
          value: 'template',
          checked: true,
        },
        {
          name: '<script>',
          value: 'script',
          checked: true,
        },
        {
          name: 'style',
          value: 'style',
          checked: true,
        },
      ],
      validate(value) {
        if (
          value.indexOf('script') === -1 &&
          value.indexOf('template') === -1
        ) {
          return 'View require at least a <script> or <template> tag.';
        }
        return true;
      },
    },
  ],
  actions(data) {
    const { name } = data;
    const zhName = data.zh_name;
    const properCaseName = '{{ properCase name }}';
    return [
      {
        type: 'add',
        path: `src/views/${name}/index.vue`,
        templateFile: 'plop-templates/view/index.hbs',
        data: {
          name,
          zhName,
          template: data.blocks.includes('template'),
          script: data.blocks.includes('script'),
          style: data.blocks.includes('style'),
        },
      },
      {
        type: 'add',
        path: `src/views/${name}/components/${properCaseName}AddModal.vue`,
        templateFile: 'plop-templates/view/add-modal.hbs',
        data: {
          name,
          zhName,
          modalTitle: '{{ title }}',
          template: data.blocks.includes('template'),
          script: data.blocks.includes('script'),
          style: data.blocks.includes('style'),
        },
      },

      {
        type: 'add',
        path: `src/views/${name}/components/${properCaseName}DetailDrawer.vue`,
        templateFile: 'plop-templates/view/detail-drawer.hbs',
        data: {
          name,
          template: data.blocks.includes('template'),
          script: data.blocks.includes('script'),
          style: data.blocks.includes('style'),
        },
      },
      {
        type: 'add',
        path: `src/router/routes/modules/${name}.ts`,
        templateFile: 'plop-templates/view/router.hbs',
        data: {
          name,
        },
      },
      {
        type: 'add',
        path: `src/api/${name}.ts`,
        templateFile: 'plop-templates/view/api.hbs',
        data: {
          name,
        },
      },
    ];
  },
};
