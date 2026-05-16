import axios from 'axios';

/**
 * 发送手机验证码
 */
export function sendMobileCode(mobile: string) {
  return axios.post('/api/verification/send-mobile-code', { mobile });
}

/**
 * 发送邮箱验证码
 */
export function sendEmailCode(email: string) {
  return axios.post('/api/verification/send-email-code', { email });
}
