import axios from 'axios';

export interface SetupStatus {
  needs_setup: boolean;
  installed: boolean;
  step: string;
}

export interface SetupInstallRequest {
  database: {
    url: string;
  };
  redis: {
    url: string;
  };
  admin: {
    email: string;
    display_name: string;
    password: string;
    confirm_password: string;
  };
  server: {
    addr: string;
    log_level: string;
  };
}

export function getSetupStatus() {
  return axios.get<SetupStatus>('/setup/status');
}

export function testDatabase(data: SetupInstallRequest['database']) {
  return axios.post('/setup/test-db', data);
}

export function testRedis(data: SetupInstallRequest['redis']) {
  return axios.post('/setup/test-redis', data);
}

export function install(data: SetupInstallRequest) {
  return axios.post('/setup/install', data);
}
