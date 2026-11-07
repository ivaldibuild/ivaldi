import { Component, effect, inject, signal, untracked } from '@angular/core';
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
import { CurrentUser } from '../api/current-user';
import { Team } from '../api/models';
import { TeamsApi } from '../api/teams-api';
import { PageHeader } from '../delivery/page-header';
import { RequestStatus } from '../shared/request-status';

/**
 * The Organisation's Teams: the groups of people and Agents that own Projects and Applications.
 * Owners and admins create them here.
 */
@Component({
  selector: 'app-teams-page',
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
  templateUrl: './teams-page.html',
})
export class TeamsPage {
  private readonly api = inject(TeamsApi);
  private readonly router = inject(Router);
  protected readonly user = inject(CurrentUser);

  protected readonly teams = signal<Team[]>([]);
  protected readonly showArchived = signal(false);
  protected readonly status = new RequestStatus();

  protected readonly creating = signal(false);
  protected readonly createStatus = new RequestStatus();
  private readonly model = signal({ display_name: '', description: '' });
  protected readonly createForm = form(this.model, (path) => {
    required(path.display_name, { message: 'Name the Team, such as Payments.' });
  });

  constructor() {
    effect(() => {
      const organizationId = this.user.organization()?.id;
      const includeArchived = this.showArchived();
      if (organizationId) {
        untracked(() => void this.load(includeArchived));
      }
    });
  }

  protected memberCount(team: Team): string {
    const n = team.members.length;
    return `${n} ${n === 1 ? 'member' : 'members'}`;
  }

  protected async create(event: Event): Promise<void> {
    event.preventDefault();
    await submit(this.createForm, async () => {
      const { display_name, description } = this.model();
      let created: Team | undefined;
      const ok = await this.createStatus.run(async () => {
        created = await this.api.create({
          display_name: display_name.trim(),
          ...(description.trim() ? { description: description.trim() } : {}),
        });
      });
      if (ok && created) {
        // Its page is where it gets its members, so go straight there.
        await this.router.navigate(['/settings/teams', created.id]);
      }
      return undefined;
    });
  }

  protected cancelCreate(): void {
    this.creating.set(false);
    this.createStatus.error.set(null);
    this.createForm().reset({ display_name: '', description: '' });
  }

  private async load(includeArchived: boolean): Promise<void> {
    await this.status.run(async () => {
      this.teams.set(await this.api.list(includeArchived));
    }, 'loading');
  }
}
