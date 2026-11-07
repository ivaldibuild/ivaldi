import { Component, computed, effect, inject, input, signal, untracked } from '@angular/core';
import { form, FormField, required, submit } from '@angular/forms/signals';
import { RouterLink } from '@angular/router';
import { HlmAlertImports } from '@spartan-ng/helm/alert';
import { HlmBadgeImports } from '@spartan-ng/helm/badge';
import { HlmButtonImports } from '@spartan-ng/helm/button';
import { HlmFieldImports } from '@spartan-ng/helm/field';
import { HlmInputImports } from '@spartan-ng/helm/input';
import { HlmSpinnerImports } from '@spartan-ng/helm/spinner';
import { CurrentUser } from '../api/current-user';
import { Person, Team, TeamMember, TeamRole } from '../api/models';
import { teamRoleLabels, TeamsApi } from '../api/teams-api';
import { PageHeader } from '../delivery/page-header';
import { RequestStatus } from '../shared/request-status';

interface MemberRow {
  member: TeamMember;
  name: string;
  /** A person's email, or what kind of member it is. */
  detail: string;
}

const teamRoles: TeamRole[] = ['lead', 'member'];

/**
 * One Team: its name and description, archiving and restoring it, and its members, people and
 * Agents, with their roles on it. Owners and admins change it; everyone else reads it.
 */
@Component({
  selector: 'app-team-page',
  imports: [
    FormField,
    PageHeader,
    RouterLink,
    HlmAlertImports,
    HlmBadgeImports,
    HlmButtonImports,
    HlmFieldImports,
    HlmInputImports,
    HlmSpinnerImports,
  ],
  templateUrl: './team-page.html',
})
export class TeamPage {
  readonly id = input.required<string>();

  private readonly api = inject(TeamsApi);
  protected readonly user = inject(CurrentUser);

  protected readonly status = new RequestStatus();
  protected readonly team = signal<Team | null>(null);
  private readonly people = signal<Person[]>([]);
  /** Names of Agents created here; the API has no other way to name an Agent yet. */
  private readonly agentNames = signal<ReadonlyMap<string, string>>(new Map());

  protected readonly teamRoles = teamRoles;
  protected readonly roleLabels = teamRoleLabels;

  /** Owners and admins change Teams, unless the Team is archived. */
  protected readonly canEdit = computed(
    () => this.user.canManageOrganization() && !this.team()?.archived_at,
  );

  protected readonly members = computed<MemberRow[]>(() => {
    const people = new Map(this.people().map((p) => [p.id, p]));
    const agents = this.agentNames();
    return (this.team()?.members ?? []).map((member) => {
      if (member.principal_kind === 'agent') {
        return {
          member,
          name: agents.get(member.principal_id) ?? member.principal_id,
          detail: 'Agent',
        };
      }
      const person = people.get(member.principal_id);
      return {
        member,
        name: person?.display_name ?? member.principal_id,
        detail: person?.email ?? 'Person',
      };
    });
  });

  /** Active people who aren't members yet. */
  protected readonly candidates = computed(() => {
    const members = new Set((this.team()?.members ?? []).map((m) => m.principal_id));
    return this.people().filter((p) => p.status === 'active' && !members.has(p.id));
  });

  // The Team's details.
  protected readonly detailsStatus = new RequestStatus();
  private readonly detailsModel = signal({ display_name: '', description: '' });
  protected readonly detailsForm = form(this.detailsModel, (path) => {
    required(path.display_name, { message: 'Name the Team.' });
  });
  protected readonly detailsChanged = computed(() => {
    const team = this.team();
    const { display_name, description } = this.detailsModel();
    return (
      !!team &&
      (display_name.trim() !== team.display_name || description.trim() !== (team.description ?? ''))
    );
  });

  // Archiving and restoring.
  protected readonly archiveStatus = new RequestStatus();

  // Members.
  protected readonly memberStatus = new RequestStatus();
  protected readonly changing = signal<string | null>(null);
  protected readonly personToAdd = signal('');
  protected readonly roleToAdd = signal<TeamRole>('member');

  // Agents.
  protected readonly addingAgent = signal(false);
  protected readonly agentStatus = new RequestStatus();
  private readonly agentModel = signal({ display_name: '', role: 'member' as TeamRole });
  protected readonly agentForm = form(this.agentModel, (path) => {
    required(path.display_name, { message: 'Name the Agent, such as Release helper.' });
  });

  constructor() {
    effect(() => {
      const id = this.id();
      if (this.user.organization()?.id) {
        untracked(() => void this.load(id));
      }
    });
  }

  protected async saveDetails(event: Event): Promise<void> {
    event.preventDefault();
    await submit(this.detailsForm, async () => {
      const { display_name, description } = this.detailsModel();
      await this.detailsStatus.run(async () =>
        this.show(
          await this.api.update(this.id(), {
            display_name: display_name.trim(),
            description: description.trim(),
          }),
        ),
      );
      return undefined;
    });
  }

  protected async archive(): Promise<void> {
    await this.archiveStatus.run(async () => this.show(await this.api.archive(this.id())));
  }

  protected async restore(): Promise<void> {
    await this.archiveStatus.run(async () => this.show(await this.api.restore(this.id())));
  }

  protected async changeRole(row: MemberRow, role: TeamRole): Promise<void> {
    if (role === row.member.role) {
      return;
    }
    this.changing.set(row.member.principal_id);
    await this.memberStatus.run(async () =>
      this.team.set(await this.api.setMember(this.id(), row.member.principal_id, role)),
    );
    this.changing.set(null);
  }

  protected async remove(row: MemberRow): Promise<void> {
    this.changing.set(row.member.principal_id);
    await this.memberStatus.run(async () =>
      this.team.set(await this.api.removeMember(this.id(), row.member.principal_id)),
    );
    this.changing.set(null);
  }

  protected async addPerson(event: Event): Promise<void> {
    event.preventDefault();
    const personId = this.personToAdd();
    if (!personId) {
      this.memberStatus.error.set('Choose a person to add.');
      return;
    }
    const added = await this.memberStatus.run(async () =>
      this.team.set(await this.api.setMember(this.id(), personId, this.roleToAdd())),
    );
    if (added) {
      this.personToAdd.set('');
      this.roleToAdd.set('member');
    }
  }

  protected async addAgent(event: Event): Promise<void> {
    event.preventDefault();
    await submit(this.agentForm, async () => {
      const { display_name, role } = this.agentModel();
      const ok = await this.agentStatus.run(async () => {
        const created = await this.api.createAgent(this.id(), {
          display_name: display_name.trim(),
          role,
        });
        this.agentNames.update((names) =>
          new Map(names).set(created.agent.id, created.agent.display_name),
        );
        this.team.set(created.team);
      });
      if (ok) {
        this.cancelAgent();
      }
      return undefined;
    });
  }

  protected cancelAgent(): void {
    this.addingAgent.set(false);
    this.agentStatus.error.set(null);
    this.agentForm().reset({ display_name: '', role: 'member' });
  }

  private show(team: Team): void {
    this.team.set(team);
    this.detailsForm().reset({
      display_name: team.display_name,
      description: team.description ?? '',
    });
  }

  private async load(id: string): Promise<void> {
    await this.status.run(async () => {
      const [team, people] = await Promise.all([this.api.get(id), this.api.people()]);
      this.people.set(people);
      this.show(team);
    }, 'loading');
  }
}
