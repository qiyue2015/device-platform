import debug from './env';

export default ({ mock, setup }: { mock?: boolean; setup: () => void }) => {
  if (mock !== false && debug) setup();
};

export const successResponseWrap = (data: unknown) => {
  return {
    success: true,
    status: 200,
    code: 0,
    message: 'ok',
    data,
    meta: null,
    request_id: '',
  };
};

export const successPaginationResponseWrap = (response: any) => {
  const { data, meta } = response;
  return {
    success: true,
    status: 200,
    code: 0,
    message: 'ok',
    data,
    meta,
    request_id: '',
  };
};

export const failResponseWrap = (data: unknown, message: string, code = 50000) => {
  return {
    success: false,
    status: 400,
    code,
    error_code: String(code),
    message,
    data,
    request_id: '',
  };
};
