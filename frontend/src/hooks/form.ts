import { reactive } from 'vue';
import { cloneDeep } from 'lodash';

export default function useForm<F extends object>(initialValues?: F | null) {
  // 获取初始值的深拷贝，确保每次重置时不受引用的影响。如果 `initialValues` 为空，则返回一个空对象。
  const getInitialValues = (): F => (initialValues ? cloneDeep(initialValues) : ({} as F));

  // 创建响应式的表单对象
  const formData: Record<string, any> = reactive(getInitialValues());

  const resetForm = () => {
    Object.keys(formData).forEach((key) => {
      delete formData[key];
    });
    Object.assign(formData, getInitialValues());
  };

  return {
    formData,
    resetForm,
  };
}
