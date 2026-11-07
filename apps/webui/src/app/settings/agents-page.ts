import { Component, computed, effect, inject, signal, untracked } from '@angular/core';
import { form, FormField, required, submit } from '@angular/forms/signals';
import { Router, RouterLink } from '@angular/router';
import { NgIcon, provideIcons } from '@ng-icons/core';
import { lucidePlus } from '@ng-icons/lucide';
import { HlmAlertImports } from '@spartan-ng/helm/alert';
import { HlmBadgeImports } from '@spartan-ng/helm/badge';
import { HlmButtonImports } from '@spartan-ng/helm/button';
import { HlmFieldImports } from '@spartan-ng/helm/field';
import { HlmInputImports } from '@spartan-ng/helm/input';
import { HlmSpinnerImports } from '@spartan-ng/helm/spinner';
import { AgentsApi } from '../api/agents-api';
import { CurrentUser } from '../api/current-user';
import { AgentDetail, AgentSummary, Team, TeamRole } from '../api/models';
import { teamRoleLabels, TeamsApi } from '../api/teams-api';
import { PageHeader } from '../delivery/page-header';
import { formatDay } from '../shared/dates';
import { RequestStatus } from '../shared/request-status';

const teamRoles: TeamRole[] = ['lead', 'member'];

/**
 * The Organisation's Agents: durable identities that belong to Teams and act with their roles.
 * Their page holds the system prompt that tells every Session how they should work.
 */
@Component({
  selector: 'app-agents-page',
  imports: [
    FormField,
    NgIcon,
    PageHeader,
    RouterLink,
    HlmAlertImports,
    HlmBadgeImports,
    HlmButtonImports,
    HlmFieldImports,
    HlmInputImports,
    HlmSpinnerImports,
  ],
  providers: [provideIcons({ lucidePlus })],
  templateUrl: './agents-page.html',
})
export class AgentsPage {
  private readonly api = inject(AgentsApi);
  private readonly teamsApi = inject(TeamsApi);
  private readonly router = inject(Router);
  protected readonly user = inject(CurrentUser);

  protected readonly agents = signal<AgentSummary[]>([]);
  protected readonly teams = signal<Team[]>([]);
  protected readonly showSuspended = signal(false);
  protected readonly status = new RequestStatus();

  protected readonly creating = signal(false);
  protected readonly createStatus = new RequestStatus();
  protected readonly teamRoles = teamRoles;
  protected readonly roleLabels = teamRoleLabels;
  protected readonly formatDay = formatDay;

  private readonly model = signal({
    display_name: '',
    slug: '',
    team_id: '',
    role: 'member' as TeamRole,
    system_prompt: '',
  });
  protected readonly createForm = form(this.model, (path) => {
    required(path.display_name, { message: 'Name the Agent, such as Release helper.' });
    required(path.team_id, { message: 'Choose the Team the Agent joins first.' });
  });

  /** Active Teams an Agent can join. */
  protected readonly activeTeams = computed(() => this.teams().filter((team) => !team.archived_at));

  constructor() {
    effect(() => {
      const organizationId = this.user.organization()?.id;
      const includeSuspended = this.showSuspended();
      if (organizationId) {
        untracked(() => void this.load(includeSuspended));
      }
    });
  }

  protected teamNames(agent: AgentSummary): string {
    return agent.teams.map((team) => team.display_name).join(', ');
  }

  protected async create(event: Event): Promise<void> {
    event.preventDefault();
    await submit(this.createForm, async () => {
      const { display_name, slug, team_id, role, system_prompt } = this.model();
      let created: AgentDetail | undefined;
      const ok = await this.createStatus.run(async () => {
        created = await this.api.create({
          display_name: display_name.trim(),
          team_id,
          role,
          ...(slug.trim() ? { slug: slug.trim() } : {}),
          ...(system_prompt.trim() ? { system_prompt: system_prompt } : {}),
        });
      });
      if (ok && created) {
        await this.router.navigate(['/settings/agents', created.id]);
      }
      return undefined;
    });
  }

  protected cancelCreate(): void {
    this.creating.set(false);
    this.createStatus.error.set(null);
    this.createForm().reset({
      display_name: '',
      slug: '',
      team_id: '',
      role: 'member',
      system_prompt: '',
    });
  }

  private async load(includeSuspended: boolean): Promise<void> {
    await this.status.run(async () => {
      const [agents, teams] = await Promise.all([
        this.api.list(includeSuspended),
        this.teamsApi.list(),
      ]);
      this.agents.set(agents);
      this.teams.set(teams);
      if (!this.model().team_id) {
        const first = teams.find((team) => !team.archived_at);
        if (first) {
          this.model.update((model) => ({ ...model, team_id: first.id }));
        }
      }
    }, 'loading');
  }
}
