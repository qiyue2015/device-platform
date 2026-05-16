import axios from 'axios';

export interface FileRecord {
  id: string;
  name: string;
  path: string;
  url: string;
}

export interface FileParams {
  current: number;
  pageSize: number;
}

// 列表返回数据
export interface FilesResponse {
  data: FileRecord[];
  meta: {
    page: number;
    page_size: number;
    has_more: boolean;
    total: number;
  };
}

// 图片列表
export function queryFiles(params: FileParams) {
  return axios.get<any, FilesResponse>('/api/file/images', {
    params,
  });
}

// 批量删除文件
export function deleteFiles(ids: string[]) {
  return axios.delete(`/api/files`, {
    data: {
      file_ids: ids,
    },
  });
}
