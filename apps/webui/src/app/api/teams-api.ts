import { inject, Service } from '@angular/core';
import { ApiClient } from './api-client';
import { Agent, CursorPage, Person, Team, TeamRole } from './models';

/** The Organisation's Teams, their members, and their Agents. */
@Service()
export class TeamsApi {
  private readonly api = inject(ApiClient);

  /** Every Team, following the pages; archived ones too when `includeArchived` is set. */
  async list(includeArchived = false): Promise<Team[]> {
    const teams: Team[] = [];
    let cursor: string | null = null;
    do {
      const query = new URLSearchParams({ limit: '100' });
      if (includeArchived) {
        query.set('include_archived', 'true');
      }
      if (cursor) {
        query.set('next_cursor', cursor);
      }
      const page: CursorPage<Team> = await this.api.get(`/teams?${query}`);
      teams.push(...page.items);
      cursor = page.has_next ? page.next_cursor : null;
    } while (cursor);
    return teams;
  }

  get(id: string): Promise<Team> {
    return this.api.get(`/teams/${encodeURIComponent(id)}`);
  }

  create(body: { display_name: string; description?: string }): Promise<Team> {
    return this.api.send('POST', '/teams', body);
  }

  /** Changes the name or description; an empty description removes it. */
  update(id: string, body: { display_name?: string; description?: string }): Promise<Team> {
    return this.api.send('PATCH', `/teams/${encodeURIComponent(id)}`, body);
  }

  archive(id: string): Promise<Team> {
    return this.api.send('POST', `/teams/${encodeURIComponent(id)}/archive`);
  }

  restore(id: string): Promise<Team> {
    return this.api.send('POST', `/teams/${encodeURIComponent(id)}/restore`);
  }

  /** Adds a member, or changes a member's role. */
  setMember(teamId: string, principalId: string, role: TeamRole): Promise<Team> {
    return this.api.send(
      'PUT',
      `/teams/${encodeURIComponent(teamId)}/members/${encodeURIComponent(principalId)}`,
      { role },
    );
  }

  removeMember(teamId: string, principalId: string): Promise<Team> {
    return this.api.send(
      'DELETE',
      `/teams/${encodeURIComponent(teamId)}/members/${encodeURIComponent(principalId)}`,
    );
  }

  /** Creates an Agent as a member of the Team; Agents exist only through a Team. */
  createAgent(
    teamId: string,
    body: { display_name: string; role?: TeamRole },
  ): Promise<{ agent: Agent; team: Team }> {
    return this.api.send('POST', `/teams/${encodeURIComponent(teamId)}/agents`, body);
  }

  /** Every person in the Organisation, suspended ones included, following the pages. */
  async people(): Promise<Person[]> {
    const people: Person[] = [];
    let cursor: string | null = null;
    do {
      const query = new URLSearchParams({ limit: '100', include_inactive: 'true' });
      if (cursor) {
        query.set('next_cursor', cursor);
      }
      const page: CursorPage<Person> = await this.api.get(`/people?${query}`);
      people.push(...page.items);
      cursor = page.has_next ? page.next_cursor : null;
    } while (cursor);
    return people;
  }
}

export const teamRoleLabels: Record<TeamRole, string> = { lead: 'Lead', member: 'Member' };
