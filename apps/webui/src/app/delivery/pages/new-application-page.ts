import { Component, computed, effect, inject, resource, signal } from '@angular/core';
import { form, FormField, minLength, required, submit } from '@angular/forms/signals';
import { Router, RouterLink } from '@angular/router';
import { HlmAlertImports } from '@spartan-ng/helm/alert';
import { HlmButtonImports } from '@spartan-ng/helm/button';
import { HlmFieldImports } from '@spartan-ng/helm/field';
import { HlmInputImports } from '@spartan-ng/helm/input';
import { HlmSpinnerImports } from '@spartan-ng/helm/spinner';
import { Project } from '../../api/models';
import { RequestStatus } from '../../shared/request-status';
import { DeliveryApi, slugify } from '../delivery-api';
import { PageHeader } from '../page-header';
import { WorkspaceContext } from '../workspace-context';

/** The project select's choice for creating a Project along with the Application. */
const newProject = 'new';

/** Creates an Application, and its Project when the Workspace has none to put it in. */
@Component({
  selector: 'app-new-application-page',
  imports: [
    FormField,
    RouterLink,
    PageHeader,
    HlmAlertImports,
    HlmButtonImports,
    HlmFieldImports,
    HlmInputImports,
    HlmSpinnerImports,
  ],
  templateUrl: './new-application-page.html',
})
export class NewApplicationPage {
  private readonly api = inject(DeliveryApi);
  private readonly router = inject(Router);
  protected readonly context = inject(WorkspaceContext);

  protected readonly newProject = newProject;
  protected readonly status = new RequestStatus();
  protected readonly projectNameError = signal<string | null>(null);
  private created: Project | null = null;

  protected readonly choices = resource({
    params: () => this.context.current()?.id,
    loader: async ({ params: workspaceId }) => {
      const [projects, teams, lifecycles] = await Promise.all([
        this.api.projects(workspaceId),
        this.api.teams(),
        this.api.lifecycles(workspaceId),
      ]);
      return { projects, teams, lifecycles };
    },
  });

  protected readonly projects = computed(() => this.choices.value()?.projects ?? []);
  protected readonly teams = computed(() => this.choices.value()?.teams ?? []);
  protected readonly lifecycles = computed(() => this.choices.value()?.lifecycles ?? []);

  private readonly model = signal({
    project_id: '',
    project_name: '',
    display_name: '',
    owner_team_id: '',
    lifecycle_id: '',
  });
  protected readonly applicationForm = form(this.model, (path) => {
    required(path.project_id, { message: 'Choose a Project.' });
    minLength(path.display_name, 3, { message: 'Use at least 3 characters.' });
    required(path.owner_team_id, { message: 'Choose the Team that owns it.' });
    required(path.lifecycle_id, { message: 'Choose the Lifecycle its Releases follow.' });
  });

  protected readonly creatingProject = computed(() => this.model().project_id === newProject);

  constructor() {
    // Start from the first Project (or a new one), its owning Team, and the Standard Lifecycle.
    effect(() => {
      const choices = this.choices.value();
      if (!choices) {
        return;
      }
      const project = choices.projects[0];
      const lifecycle =
        choices.lifecycles.find((l) => l.slug === 'standard') ?? choices.lifecycles[0];
      this.model.update((m) => ({
        ...m,
        project_id: m.project_id || (project?.id ?? newProject),
        owner_team_id: m.owner_team_id || (project?.owner_team_id ?? choices.teams[0]?.id ?? ''),
        lifecycle_id: m.lifecycle_id || (lifecycle?.id ?? ''),
      }));
    });
  }

  protected projectChanged(): void {
    const project = this.projects().find((p) => p.id === this.model().project_id);
    if (project) {
      this.model.update((m) => ({ ...m, owner_team_id: project.owner_team_id }));
    }
  }

  protected async create(event: Event): Promise<void> {
    event.preventDefault();
    const workspace = this.context.current();
    const model = this.model();
    const projectName = model.project_name.trim();
    this.projectNameError.set(
      this.creatingProject() && projectName.length === 0 ? 'Name the Project.' : null,
    );
    if (!workspace || this.projectNameError()) {
      return;
    }
    await submit(this.applicationForm, async () => {
      let applicationId = '';
      const done = await this.status.run(async () => {
        let projectId = model.project_id;
        if (projectId === newProject) {
          this.created ??= await this.api.createProject(workspace.id, {
            display_name: projectName,
            slug: slugify(projectName),
            owner_team_id: model.owner_team_id,
          });
          projectId = this.created.id;
        }
        const application = await this.api.createApplication(projectId, {
          display_name: model.display_name.trim(),
          owner_team_id: model.owner_team_id,
          default_lifecycle_id: model.lifecycle_id,
        });
        applicationId = application.id;
      });
      if (done) {
        await this.router.navigate(['/applications', applicationId]);
      }
      return undefined;
    });
  }
}
