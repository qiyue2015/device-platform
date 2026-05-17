import axios from 'axios';
import type { AxiosRequestConfig, AxiosResponse } from 'axios';
import { Message, Modal } from '@arco-design/web-vue';
import { useUserStore } from '@/store';
import { getToken } from '@/utils/auth';

export interface PaginationMeta {
  pagination: string;
  page: number;
  page_size: number;
  has_more: boolean;
  total: number;
}

export interface HttpResponse<T = unknown> {
  success: boolean;
  status: number;
  message: string;
  code: number;
  data: T;
  meta?: PaginationMeta;
  request_id?: string;
}

type RequestConfigWithSilentError = AxiosRequestConfig & {
  silentError?: boolean;
};

if (import.meta.env.VITE_API_BASE_URL) {
  axios.defaults.baseURL = import.meta.env.VITE_API_BASE_URL;
}

axios.interceptors.request.use(
  (config: AxiosRequestConfig) => {
    // let each request carry token
    // this example using the JWT token
    // Authorization is a custom headers key
    // please modify it according to the actual situation
    const token = getToken();
    if (token) {
      if (!config.headers) {
        config.headers = {};
      }
      config.headers.Authorization = `Bearer ${token}`;
    }
    // 定制分页参数
    if (config.params?.current) {
      config.params.page = config.params.current;
      delete config.params.current;
    }
    if (config.params?.pageSize) {
      config.params.page_size = config.params.pageSize;
      delete config.params.pageSize;
    }
    return config;
  },
  (error) => {
    // do something
    return Promise.reject(error);
  }
);
// add response interceptors
axios.interceptors.response.use(
  (response: AxiosResponse<HttpResponse>) => {
    const res = response.data;
    if (!res || typeof res.code === 'undefined') {
      return {
        success: true,
        status: response.status,
        message: 'OK',
        code: 0,
        data: res,
      };
    }
    // if the custom code is not 0, it is judged as an error.
    if (res.code !== 0) {
      if (!(response.config as RequestConfigWithSilentError).silentError) {
        Message.error({
          content: res.message || 'Error',
          duration: 5 * 1000,
        });
      }
      // 50008: Illegal token; 50012: Other clients logged in; 50014: Token expired;
      if (res.code === -1 && response.config.url !== '/api/user/info') {
        Modal.error({
          title: 'Confirm logout',
          content: 'You have been logged out, you can cancel to stay on this page, or log in again',
          okText: 'Re-Login',
          async onOk() {
            const userStore = useUserStore();

            await userStore.logout();
            window.location.reload();
          },
        });
      }
      return Promise.reject(new Error(res.message || 'Error'));
    }
    return res;
  },
  (error: any) => {
    let errorMessage = error.message || 'Request Error';
    if (error.response && error.response.data) {
      const { data } = error.response as any;
      if (data.errors && Object.keys(data.errors).length > 0) {
        // Show the first validation error
        const [firstError] = Object.values(data.errors)[0] as string[];
        errorMessage = firstError;
      } else if (data.message) {
        errorMessage = data.message;
      }
    }

    if (!(error.config as RequestConfigWithSilentError | undefined)?.silentError) {
      Message.error({
        content: errorMessage,
        duration: 5 * 1000,
      });
    }
    return Promise.reject(error);
  }
);
