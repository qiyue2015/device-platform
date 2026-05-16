/* eslint-disable no-shadow */
export enum LoginStrategy {
  OIDC = 'oidc',
  LOCAL = 'local',
}

export interface AuthConfig {
  defaultStrategy: LoginStrategy;
}
