<template>
  <div class="container">
    <BrandBanner
      :title="t('workplace.banner.title')"
      thumb="https://lf-cdn-tos.bytescm.com/obj/static/toutiao/iaasng/cimages/1.0.0.81/ecs_overview/banner.svg"
      :description="t('workplace.banner.description')"
    />

    <a-typography-title :heading="5">{{ t('workplace.guide.title') }}</a-typography-title>
    <a-space direction="vertical" size="large">
      <Guide>
        <GuideItem step="01" :title="t('workplace.guide.step1.title')">
          {{ t('workplace.guide.step1.desc') }}
          <template #content>
            <a-button size="small" type="primary">{{ t('workplace.guide.step1.action') }}</a-button>
          </template>
        </GuideItem>
        <GuideItem step="02" :title="t('workplace.guide.step2.title')">
          {{ t('workplace.guide.step2.desc') }}
          <template #content>
            <a-link href="link">{{ t('workplace.guide.step2.action') }}</a-link>
          </template>
        </GuideItem>
        <GuideItem step="03" :title="t('workplace.guide.step3.title')">
          {{ t('workplace.guide.step3.desc') }}
          <template #content>
            <a-link href="link">{{ t('workplace.guide.step3.action') }}</a-link>
          </template>
        </GuideItem>
      </Guide>
      <a-row :gutter="16">
        <a-col :span="16">
          <a-typography-title :heading="5">{{ t('workplace.product.title') }}</a-typography-title>
          <ProductAdvance :items="products" />
        </a-col>
        <a-col :span="8">
          <a-typography-title :heading="5">{{ t('workplace.faq.title') }}</a-typography-title>
          <div class="question">
            <a v-for="(row, index) in questions" :key="index" :href="row.link" target="_blank">
              {{ row.title }}
            </a>
          </div>
        </a-col>
      </a-row>
    </a-space>
  </div>
</template>

<script lang="ts" setup>
  import { useI18n } from 'vue-i18n';
  import BrandBanner from '@/components/brand-banner/index.vue';
  import Guide from './components/guide.vue';
  import GuideItem from './components/guide-item.vue';
  import ProductAdvance from './components/product-advance.vue';

  const { t } = useI18n();

  const products = [
    {
      title: t('workplace.product.performance.title'),
      description: t('workplace.product.performance.desc'),
    },
    {
      title: t('workplace.product.flexible.title'),
      description: t('workplace.product.flexible.desc'),
    },
    {
      title: t('workplace.product.stable.title'),
      description: t('workplace.product.stable.desc'),
    },
    {
      title: t('workplace.product.secure.title'),
      description: t('workplace.product.secure.desc'),
    },
  ];

  const questions = [
    {
      title: '如何查看某个地域或可用区是否能购买实例？',
      link: 'https://www.aliyun.com/help/faq-detail/123456',
    },
    {
      title: '购买云服务器完成后是否可以切换地域？',
      link: 'https://www.aliyun.com/help/faq-detail/123457',
    },
    {
      title: '如何选择地域？',
      link: 'https://www.aliyun.com/help/faq-detail/123458',
    },
    {
      title: '如何选择可用区？',
      link: 'https://www.aliyun.com/help/faq-detail/123459',
    },
    {
      title: '如何选择适合我业务的ECS实例？',
      link: 'https://www.aliyun.com/help/faq-detail/123460',
    },
    {
      title: '购买云服务器ECS，如何选择实例类型？',
      link: 'https://www.aliyun.com/help/faq-detail/123461',
    },
    {
      title: '登录实例需要放通什么端口？',
      link: 'https://www.aliyun.com/help/faq-detail/123462',
    },
    {
      title: '哪些实例规格不支持变更？',
      link: 'https://www.aliyun.com/help/faq-detail/123463',
    },
    {
      title: '包年包月实例支持删除操作吗？',
      link: 'https://www.aliyun.com/help/faq-detail/123464',
    },
  ];
</script>

<script lang="ts">
  export default {
    name: 'Dashboard', // If you want the include property of keep-alive to take effect, you must name the component
  };
</script>

<style lang="less" scoped>
  .container {
    min-width: 1000px;
    max-width: 1240px;
    margin: 0 auto;
    padding: 32px;
  }

  .question {
    a {
      display: block;
      margin-bottom: 8px;
      padding: 0;
      color: var(--color-text-2);
      font-size: 13px;
      line-height: 22px;
      text-decoration: none;
      background-color: transparent;
      border-radius: var(--border-radius-small);
      transition: all 0.1s linear;
    }
  }
</style>
