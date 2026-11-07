import { inject, Service } from '@angular/core';
import { ApiClient } from './api-client';
import { AgentDetail, AgentSummary, CursorPage } from './models';

/** The Organisation's Agents, their Team memberships, and system prompts. */
@Service()
export class AgentsApi {
  private readonly api = inject(ApiClient);

  /** Active Agents, and suspended ones when `includeInactive` is set. */
  async list(includeInactive = false): Promise<AgentSummary[]> {
    const agents: AgentSummary[] = [];
    let cursor: string | null = null;
    do {
      const query = new URLSearchParams({ limit: '100' });
      if (includeInactive) {
        query.set('include_inactive', 'true');
      }
      if (cursor) {
        query.set('next_cursor', cursor);
      }
      const page: CursorPage<AgentSummary> = await this.api.get(`/agents?${query}`);
      agents.push(...page.items);
      cursor = page.has_next ? page.next_cursor : null;
    } while (cursor);
    return agents;
  }

  get(id: string): Promise<AgentDetail> {
    return this.api.get(`/agents/${encodeURIComponent(id)}`);
  }

  create(body: {
    display_name: string;
    slug?: string;
    team_id: string;
    role?: 'lead' | 'member';
    system_prompt?: string;
  }): Promise<AgentDetail> {
    return this.api.send('POST', '/agents', body);
  }

  /** Changes the name, the system prompt, or both; an empty prompt clears it. */
  update(
    id: string,
    body: { display_name?: string; system_prompt?: string },
  ): Promise<AgentDetail> {
    return this.api.send('PATCH', `/agents/${encodeURIComponent(id)}`, body);
  }

  suspend(id: string): Promise<AgentDetail> {
    return this.api.send('POST', `/agents/${encodeURIComponent(id)}/suspend`);
  }

  reactivate(id: string): Promise<AgentDetail> {
    return this.api.send('POST', `/agents/${encodeURIComponent(id)}/reactivate`);
  }
}
