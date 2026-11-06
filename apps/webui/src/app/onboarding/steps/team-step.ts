import { Component, inject, signal } from '@angular/core';
import { form, FormField, required, submit } from '@angular/forms/signals';
import { Router, RouterLink } from '@angular/router';
import { NgIcon, provideIcons } from '@ng-icons/core';
import { lucideBot, lucidePlus, lucideUser, lucideX } from '@ng-icons/lucide';
import { HlmAlertImports } from '@spartan-ng/helm/alert';
import { HlmButtonImports } from '@spartan-ng/helm/button';
import { HlmFieldImports } from '@spartan-ng/helm/field';
import { HlmInputImports } from '@spartan-ng/helm/input';
import { HlmSpinnerImports } from '@spartan-ng/helm/spinner';
import { CurrentUser } from '../../api/current-user';
import { Person } from '../../api/models';
import { PeopleApi } from '../../api/people-api';
import { Session } from '../../auth/session';
import { OnboardingApi, Team } from '../onboarding-api';
import { SetupStep } from '../setup-step';
import { RequestStatus } from '../../shared/request-status';

/** Step 3: the first Team, led by the person setting up, with the people and agents it needs. */
@Component({
  selector: 'app-team-step',
  imports: [
    FormField,
    NgIcon,
    RouterLink,
    SetupStep,
    HlmAlertImports,
    HlmButtonImports,
    HlmFieldImports,
    HlmInputImports,
    HlmSpinnerImports,
  ],
  providers: [provideIcons({ lucideBot, lucidePlus, lucideUser, lucideX })],
  templateUrl: './team-step.html',
})
export class TeamStep {
  private readonly api = inject(OnboardingApi);
  private readonly session = inject(Session);
  protected readonly user = inject(CurrentUser);
  private readonly router = inject(Router);

  protected readonly status = new RequestStatus();
  protected readonly teams = signal<Team[]>([]);
  protected readonly agents = signal<string[]>([]);
  protected readonly agentName = signal('');
  private created: Team | null = null;
  private joined = false;

  // Finding people who have signed in, to add as members.
  private readonly peopleApi = inject(PeopleApi);
  protected readonly people = signal<Person[]>([]);
  protected readonly personSearch = signal('');
  protected readonly matches = signal<Person[]>([]);
  protected readonly searching = signal(false);
  private searchTimer: ReturnType<typeof setTimeout> | undefined;

  private readonly model = signal({ display_name: '' });
  protected readonly teamForm = form(this.model, (path) => {
    required(path.display_name, { message: 'Name the Team.' });
  });

  constructor() {
    void this.status.run(async () => this.teams.set(await this.api.teams()), 'loading');
  }

  protected addAgent(): void {
    const name = this.agentName().trim();
    if (name && !this.agents().includes(name)) {
      this.agents.update((agents) => [...agents, name]);
    }
    this.agentName.set('');
  }

  protected removeAgent(name: string): void {
    this.agents.update((agents) => agents.filter((a) => a !== name));
  }

  protected onPersonSearch(value: string): void {
    this.personSearch.set(value);
    clearTimeout(this.searchTimer);
    if (value.trim().length < 2) {
      this.matches.set([]);
      return;
    }
    this.searchTimer = setTimeout(() => void this.findPeople(value), 300);
  }

  protected addPerson(person: Person): void {
    if (!this.people().some((p) => p.id === person.id)) {
      this.people.update((people) => [...people, person]);
    }
    this.personSearch.set('');
    this.matches.set([]);
  }

  protected removePerson(id: string): void {
    this.people.update((people) => people.filter((p) => p.id !== id));
  }

  private async findPeople(search: string): Promise<void> {
    this.searching.set(true);
    try {
      const page = await this.peopleApi.people({ search, limit: 6 });
      const me = this.session.principalId();
      const chosen = new Set(this.people().map((p) => p.id));
      // Keep only results for the latest search, without the lead and anyone already added.
      if (search === this.personSearch()) {
        this.matches.set(page.items.filter((p) => p.id !== me && !chosen.has(p.id)));
      }
    } catch {
      this.matches.set([]);
    } finally {
      this.searching.set(false);
    }
  }

  protected onAgentKey(event: KeyboardEvent): void {
    if (event.key === 'Enter') {
      event.preventDefault();
      this.addAgent();
    }
  }

  protected async create(event: Event): Promise<void> {
    event.preventDefault();
    await submit(this.teamForm, async () => {
      // A retry after a partial failure picks up where the last attempt stopped.
      const created = await this.status.run(async () => {
        let team = this.created;
        if (!team) {
          team = await this.api.createTeam({ display_name: this.model().display_name.trim() });
          this.created = team;
        }
        const me = this.session.principalId();
        if (me && !this.joined) {
          await this.api.setTeamMember(team.id, me, 'lead');
          this.joined = true;
        }
        for (const person of this.people()) {
          await this.api.setTeamMember(team.id, person.id, 'member');
          this.removePerson(person.id);
        }
        for (const agent of this.agents()) {
          await this.api.createAgent(team.id, agent);
          this.removeAgent(agent);
        }
      });
      if (created) {
        await this.router.navigate(['/setup', 'environments']);
      }
      return undefined;
    });
  }
}
