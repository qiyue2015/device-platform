import { LoginStrategy } from '@/types/auth';

export function getAuthStrategy(): LoginStrategy {
  return import.meta.env.VITE_AUTH_STRATEGY === LoginStrategy.OIDC ? LoginStrategy.OIDC : LoginStrategy.LOCAL;
}

export function isOidc(): boolean {
  return getAuthStrategy() === LoginStrategy.OIDC;
}

export function redirectToOidcLogin(): void {
  window.location.href = `${import.meta.env.VITE_API_BASE_URL}/auth/redirect`;
}
