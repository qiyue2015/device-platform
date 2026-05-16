<script setup lang="ts">
  import { computed, reactive, ref } from 'vue';
  import { CascaderOption } from '@arco-design/web-vue';
  import type { TableColumnData } from '@arco-design/web-vue/es/table/interface';
  import { jsonp } from 'vue-jsonp';
  import { useI18n } from 'vue-i18n';
  import { useLoading, useVisible } from '@/hooks';

  const { t } = useI18n();
  const emits = defineEmits(['update:modelValue', 'change']);

  const props = defineProps({
    modelValue: {
      type: String,
      default: null,
    },
    appKey: {
      type: String,
      default: null,
    },
  });

  /**
   * jsonp 获取省市区 district
   */
  const getDistrict = async () => {
    const cacheKey = 'qq-map-district';
    const areaData = localStorage.getItem(cacheKey);
    if (areaData) {
      return JSON.parse(areaData);
    }

    const response = await jsonp('https://apis.map.qq.com/ws/district/v1/list', {
      key: props.appKey,
      output: 'jsonp',
    });

    if (response.status === 0) {
      localStorage.setItem(cacheKey, JSON.stringify(response));
    }

    return response;
  };

  /**
   * jsonp 获取当前位置
   * https://lbs.qq.com/dev/console/demo-center
   */
  const getCurrentPosition = () => {
    return jsonp('https://apis.map.qq.com/ws/location/v1/ip', {
      key: props.appKey,
      output: 'jsonp',
    });
  };

  /**
   * 关键词输入提示
   * https://lbs.qq.com/service/webService/webServiceGuide/search/webServiceSuggestion
   */
  const getPlaceSuggestions = (param: any) => {
    return jsonp('https://apis.map.qq.com/ws/place/v1/suggestion', {
      ...param,
      key: props.appKey,
      region_fix: 1,
      policy: 1,
      output: 'jsonp',
    });
  };

  const columns = computed<TableColumnData[]>(() => [
    { title: t('common.mapSelect.columns.index'), slotName: 'index', width: 60, align: 'center', fixed: 'left' },
    { title: t('common.mapSelect.columns.address'), slotName: 'title', ellipsis: true, tooltip: true },
    { title: t('common.mapSelect.columns.select'), slotName: 'operate', width: 100, fixed: 'right' },
  ]);

  const cascaderLoading = ref(false);
  const cascaderOptions = ref<CascaderOption[]>([]);
  const initCascaderData = async () => {
    if (cascaderOptions.value.length === 0) {
      cascaderLoading.value = true;
      const response = await getDistrict();
      const provinceData = response.result[0];
      const cityData = response.result[1];
      const districtData = response.result[2];
      cascaderLoading.value = false;
      cascaderOptions.value = provinceData.map((row: any) => {
        const children = [];
        for (let i = row.cidx[0]; i <= row.cidx[1]; i += 1) {
          // 城市代码，取前4位
          const cityCode = cityData[i].id.toString().slice(0, 4);

          // 遍历区县根据城市代码前置4位匹配
          const districtChildren = districtData
            .filter((city: any) => {
              return city.id.toString().startsWith(cityCode);
            })
            .map((city: any) => ({
              label: city.fullname,
              value: city.id,
            }));

          if (districtChildren.length === 0) {
            // 没有区县的城市
            children.push({
              label: cityData[i].fullname,
              value: cityData[i].id,
            });
          } else {
            // 有区县的城市
            children.push({
              label: cityData[i].fullname,
              value: cityData[i].id,
              children: districtChildren,
            });
          }
        }
        return {
          label: row.fullname,
          value: row.id,
          children,
        };
      });
    }
  };

  const { loading, setLoading } = useLoading(false);
  const { visible, setVisible } = useVisible(false);

  const location = ref<string>(props.modelValue);
  const formData = ref<any>({ keyword: '', region: '' });
  const query = reactive({ page_index: 1, page_size: 20 });
  const pagination = reactive({ current: query.page_index, pageSize: query.page_size, total: 0, showTotal: true });
  const addressList = ref<any[]>([]);

  const fetchData = async () => {
    try {
      setLoading(true);
      const { data, count } = await getPlaceSuggestions({ ...query, ...formData.value });
      pagination.total = count;
      addressList.value = data;
    } finally {
      setLoading(false);
    }
  };

  const onPageChange = (page: number) => {
    query.page_index = page;
    pagination.current = page;
    fetchData();
  };

  const onSelect = (record: any) => {
    location.value = `${record.location.lat},${record.location.lng}`;
    emits('update:modelValue', `${record.location.lat},${record.location.lng}`);
    emits('change', record);
    setVisible(false);
  };

  const openModal = () => {
    setVisible(true);
    getCurrentPosition().then(({ result }) => {
      formData.value.region = result?.ad_info.district;
      formData.value.areaCode = String(result?.ad_info.adcode);
      initCascaderData();
    });
  };
</script>

<template>
  <div class="qq-map-container">
    <a-input-search
      v-model="location"
      :placeholder="t('common.mapSelect.placeholder')"
      :button-text="t('common.action.select')"
      search-button
      readonly
      @search="openModal"
    />
    <a-modal v-model:visible="visible" :footer="false" width="680px" title-align="start" :title="t('common.mapSelect.title')">
      <a-space direction="vertical" size="medium" fill>
        <a-row :gutter="10">
          <a-col :span="10">
            <a-cascader
              v-model="formData.areaCode"
              :loading="cascaderLoading"
              :options="cascaderOptions"
              style="width: 100%"
              expand-child
              allow-clear
              :placeholder="t('common.mapSelect.regionPlaceholder')"
            />
          </a-col>
          <a-col :span="14">
            <a-input-search
              v-model="formData.keyword"
              :loading="loading"
              allow-clear
              :placeholder="t('common.mapSelect.searchPlaceholder')"
              style="width: 100%"
              search-button
              :button-text="t('common.action.search')"
              @search="fetchData"
            />
          </a-col>
        </a-row>
        <a-table
          :loading="loading"
          :columns="columns"
          :data="addressList"
          :pagination="pagination"
          :virtual-list-props="{ height: 300 }"
          size="small"
          stripe
          @page-change="onPageChange"
        >
          <template #index="{ rowIndex }">
            <div style="width: 30px; text-align: center">{{ rowIndex + 1 }}</div>
          </template>
          <template #title="{ record }">
            <div style="color: var(--color-text-1)">{{ record.title }}</div>
            <div style="color: var(--color-text-3)">{{ record.address }}</div>
            <div style="color: var(--color-text-3)">{{ record.location.lat }}, {{ record.location.lng }}</div>
          </template>
          <template #operate="{ record }">
            <a-button type="text" size="small" @click="onSelect(record)">{{ t('common.action.select') }}</a-button>
          </template>
        </a-table>
      </a-space>
    </a-modal>
  </div>
</template>

<style scoped lang="less">
  .qq-map-container {
    width: 100%;
  }

  :deep(.arco-empty) {
    padding: 106px 0;
  }
</style>
