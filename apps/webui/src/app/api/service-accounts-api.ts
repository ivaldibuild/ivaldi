import { inject, Service } from '@angular/core';
import { ApiClient } from './api-client';
import { ApiToken, IssuedApiToken, ServiceAccount } from './models';

/** The Organisation's service accounts and their API tokens. */
@Service()
export class ServiceAccountsApi {
  private readonly api = inject(ApiClient);

  /** Active service accounts, and suspended ones when `includeInactive` is set. */
  list(includeInactive = false): Promise<ServiceAccount[]> {
    return this.api.get(`/service-accounts${includeInactive ? '?include_inactive=true' : ''}`);
  }

  get(id: string): Promise<ServiceAccount> {
    return this.api.get(`/service-accounts/${encodeURIComponent(id)}`);
  }

  create(body: { display_name: string; description?: string }): Promise<ServiceAccount> {
    return this.api.send('POST', '/service-accounts', body);
  }

  suspend(id: string): Promise<ServiceAccount> {
    return this.api.send('POST', `/service-accounts/${encodeURIComponent(id)}/suspend`);
  }

  reactivate(id: string): Promise<ServiceAccount> {
    return this.api.send('POST', `/service-accounts/${encodeURIComponent(id)}/reactivate`);
  }

  tokens(serviceAccountId: string): Promise<ApiToken[]> {
    return this.api.get(`/service-accounts/${encodeURIComponent(serviceAccountId)}/tokens`);
  }

  /** Issues a token. Its secret is in this answer and never shown again. */
  issueToken(
    serviceAccountId: string,
    body: { name: string; expires_at?: string },
  ): Promise<IssuedApiToken> {
    return this.api.send(
      'POST',
      `/service-accounts/${encodeURIComponent(serviceAccountId)}/tokens`,
      body,
    );
  }

  revokeToken(tokenId: string): Promise<ApiToken> {
    return this.api.send('POST', `/api-tokens/${encodeURIComponent(tokenId)}/revoke`);
  }
}
