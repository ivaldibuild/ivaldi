import { Component, computed, inject, linkedSignal, resource, signal } from '@angular/core';
import { RouterLink } from '@angular/router';
import { NgIcon, provideIcons } from '@ng-icons/core';
import { lucidePlus, lucideSearch } from '@ng-icons/lucide';
import { HlmAlertImports } from '@spartan-ng/helm/alert';
import { HlmBadgeImports } from '@spartan-ng/helm/badge';
import { HlmButtonImports } from '@spartan-ng/helm/button';
import { HlmInputImports } from '@spartan-ng/helm/input';
import { HlmSpinnerImports } from '@spartan-ng/helm/spinner';
import { ApiError } from '../../api/api-client';
import { Project } from '../../api/models';
import { RequestStatus } from '../../shared/request-status';
import { DeliveryApi } from '../delivery-api';
import { PageHeader } from '../page-header';
import { ProjectFilter, ProjectsApi } from '../projects-api';
import { WorkspaceContext } from '../workspace-context';

/** The Workspace's Projects, by name, with search, an owning Team filter, and archived ones on request. */
@Component({
  selector: 'app-projects-page',
  imports: [
    RouterLink,
    NgIcon,
    PageHeader,
    HlmAlertImports,
    HlmBadgeImports,
    HlmButtonImports,
    HlmInputImports,
    HlmSpinnerImports,
  ],
  providers: [provideIcons({ lucidePlus, lucideSearch })],
  templateUrl: './projects-page.html',
})
export class ProjectsPage {
  private readonly api = inject(ProjectsApi);
  private readonly delivery = inject(DeliveryApi);
  protected readonly context = inject(WorkspaceContext);

  /** The search as typed; it applies when the search form is submitted. */
  protected readonly searchDraft = signal('');
  protected readonly filter = signal<ProjectFilter>({
    search: '',
    ownerTeamId: '',
    includeArchived: false,
  });

  private readonly teamsResource = resource({ loader: () => this.delivery.teams() });
  /** The Teams, or none while they load or if they didn't. */
  protected readonly teams = computed(() =>
    this.teamsResource.hasValue() ? this.teamsResource.value() : [],
  );

  protected readonly firstPage = resource({
    params: () => {
      const workspaceId = this.context.current()?.id;
      return workspaceId ? { workspaceId, filter: this.filter() } : undefined;
    },
    loader: ({ params }) => this.api.page(params.workspaceId, params.filter),
  });

  /**
   * The loaded value, or undefined while there is none: a resource's value() throws while it's in
   * an error state, which would stop the page from rendering its error.
   */
  private readonly loaded = computed(() =>
    this.firstPage.hasValue() ? this.firstPage.value() : undefined,
  );

  /** Projects from "Show more", after the first page; a new first page starts over. */
  private readonly more = linkedSignal<unknown, Project[]>({
    source: () => this.loaded(),
    computation: () => [],
  });
  private readonly cursor = linkedSignal(() => this.loaded()?.next_cursor ?? null);
  protected readonly moreStatus = new RequestStatus();

  protected readonly projects = computed(() => [...(this.loaded()?.items ?? []), ...this.more()]);
  protected readonly total = computed(() => this.loaded()?.total_count ?? 0);
  protected readonly hasMore = computed(() => this.cursor() !== null);
  protected readonly filtered = computed(() => {
    const f = this.filter();
    return f.search !== '' || f.ownerTeamId !== '' || f.includeArchived;
  });

  protected readonly failure = computed(() => {
    const error = this.firstPage.error();
    return error ? (error instanceof ApiError ? error.message : 'Something went wrong.') : null;
  });

  protected teamName(id: string): string {
    return this.teams().find((t) => t.id === id)?.display_name ?? '';
  }

  protected search(event: Event): void {
    event.preventDefault();
    this.filter.update((f) => ({ ...f, search: this.searchDraft().trim() }));
  }

  protected filterByTeam(event: Event): void {
    const ownerTeamId = (event.target as HTMLSelectElement).value;
    this.filter.update((f) => ({ ...f, ownerTeamId }));
  }

  protected showArchived(event: Event): void {
    const includeArchived = (event.target as HTMLInputElement).checked;
    this.filter.update((f) => ({ ...f, includeArchived }));
  }

  protected clearFilters(): void {
    this.searchDraft.set('');
    this.filter.set({ search: '', ownerTeamId: '', includeArchived: false });
  }

  protected async showMore(): Promise<void> {
    const workspaceId = this.context.current()?.id;
    const cursor = this.cursor();
    if (!workspaceId || !cursor) {
      return;
    }
    await this.moreStatus.run(async () => {
      const page = await this.api.page(workspaceId, this.filter(), cursor);
      this.more.update((projects) => [...projects, ...page.items]);
      this.cursor.set(page.next_cursor);
    });
  }
}
