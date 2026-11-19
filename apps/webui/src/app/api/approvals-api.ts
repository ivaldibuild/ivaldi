import { inject, Service } from '@angular/core';
import { ApiClient } from './api-client';
import { ApprovalRequest, NamedPrincipal } from './models';

/** Requests for a person's approval, and deciding them. */
@Service()
export class ApprovalsApi {
  private readonly api = inject(ApiClient);

  approval(id: string): Promise<ApprovalRequest> {
    return this.api.get(`/approvals/${id}`);
  }

  approve(id: string, note: string): Promise<ApprovalRequest> {
    return this.api.send('POST', `/approvals/${id}/approve`, note ? { note } : {});
  }

  /** Rejecting needs a note: it tells the person who asked why. */
  reject(id: string, note: string): Promise<ApprovalRequest> {
    return this.api.send('POST', `/approvals/${id}/reject`, { note });
  }

  /** The named principals among `ids`, whatever their kind. */
  async principals(ids: string[]): Promise<NamedPrincipal[]> {
    const unique = [...new Set(ids)];
    if (unique.length === 0) {
      return [];
    }
    return this.api.get(`/principals?${new URLSearchParams({ ids: unique.join(',') })}`);
  }
}
