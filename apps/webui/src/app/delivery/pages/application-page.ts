import { Component, computed, inject, input, resource, signal } from '@angular/core';
import { form, FormField, required, submit } from '@angular/forms/signals';
import { RouterLink } from '@angular/router';
import { NgIcon, provideIcons } from '@ng-icons/core';
import { lucideArrowRight, lucidePlus } from '@ng-icons/lucide';
import { HlmAlertImports } from '@spartan-ng/helm/alert';
import { HlmBadgeImports } from '@spartan-ng/helm/badge';
import { HlmButtonImports } from '@spartan-ng/helm/button';
import { HlmFieldImports } from '@spartan-ng/helm/field';
import { HlmInputImports } from '@spartan-ng/helm/input';
import { HlmSpinnerImports } from '@spartan-ng/helm/spinner';
import { ApiError } from '../../api/api-client';
import { ArtifactKind } from '../../api/models';
import { RequestStatus } from '../../shared/request-status';
import { DeliveryApi } from '../delivery-api';
import { PageHeader } from '../page-header';
import { ReleaseList } from '../releases/release-list';

const kinds: { value: ArtifactKind; label: string }[] = [
  { value: 'container_image', label: 'Container image' },
  { value: 'helm_chart', label: 'Helm chart' },
  { value: 'archive', label: 'Archive' },
];

/**
 * One Application: its Channels and the Lifecycles they follow, its Releases, the artifacts it's
 * made of, and the Targets it's placed on.
 */
@Component({
  selector: 'app-application-page',
  imports: [
    FormField,
    NgIcon,
    RouterLink,
    PageHeader,
    ReleaseList,
    HlmAlertImports,
    HlmBadgeImports,
    HlmButtonImports,
    HlmFieldImports,
    HlmInputImports,
    HlmSpinnerImports,
  ],
  providers: [provideIcons({ lucideArrowRight, lucidePlus })],
  templateUrl: './application-page.html',
})
export class ApplicationPage {
  private readonly api = inject(DeliveryApi);

  /** The `:id` route parameter. */
  readonly id = input.required<string>();

  protected readonly kinds = kinds;

  protected readonly details = resource({
    params: () => this.id(),
    loader: async ({ params: id }) => {
      const application = await this.api.application(id);
      const ws = application.workspace_id;
      const [projects, teams, channels, artifacts, placements, lifecycles, environments] =
        await Promise.all([
          this.api.projects(ws),
          this.api.teams(),
          this.api.channels(id),
          this.api.artifacts(id),
          this.api.placements(id),
          this.api.lifecycles(ws),
          this.api.environments(ws),
        ]);
      const targets = await this.api.targetsOf(environments);
      return {
        application,
        projects,
        teams,
        channels,
        artifacts,
        placements,
        lifecycles,
        environments,
        targets,
      };
    },
  });

  protected readonly application = computed(() => this.details.value()?.application ?? null);
  protected readonly project = computed(() => {
    const d = this.details.value();
    return d?.projects.find((p) => p.id === d.application.project_id)?.display_name ?? '';
  });
  protected readonly team = computed(() => {
    const d = this.details.value();
    return d?.teams.find((t) => t.id === d.application.owner_team_id)?.display_name ?? '';
  });

  protected readonly channels = computed(() => {
    const d = this.details.value();
    if (!d) {
      return [];
    }
    return [...d.channels]
      .sort((a, b) => Number(b.is_default) - Number(a.is_default))
      .map((channel) => {
        const lifecycle = d.lifecycles.find((l) => l.id === channel.lifecycle_id);
        return {
          channel,
          lifecycle: lifecycle?.display_name ?? 'Unknown Lifecycle',
          phases: [...(lifecycle?.phases ?? [])]
            .sort((a, b) => a.position - b.position)
            .map((phase) => ({
              name: phase.name,
              environments: phase.environment_ids
                .map((id) => d.environments.find((e) => e.id === id)?.display_name)
                .filter(Boolean)
                .join(', '),
            })),
        };
      });
  });

  protected readonly artifacts = computed(() => this.details.value()?.artifacts ?? []);

  protected readonly placements = computed(() => {
    const d = this.details.value();
    if (!d) {
      return [];
    }
    return d.placements.map((placement) => {
      const target = d.targets.find((t) => t.id === placement.target_id);
      return {
        placement,
        target: target?.display_name ?? placement.target_id,
        environment:
          d.environments.find((e) => e.id === target?.environment_id)?.display_name ?? '',
        locator: target ? `${target.locator['cluster']} / ${target.locator['namespace']}` : '',
      };
    });
  });

  /** Targets the Application isn't placed on yet, labelled with their Environment. */
  protected readonly unplacedTargets = computed(() => {
    const d = this.details.value();
    if (!d) {
      return [];
    }
    const placed = new Set(d.placements.map((p) => p.target_id));
    return d.targets
      .filter((t) => !placed.has(t.id))
      .map((t) => ({
        id: t.id,
        label: `${t.display_name} · ${d.environments.find((e) => e.id === t.environment_id)?.display_name ?? ''}`,
      }));
  });

  protected readonly failure = computed(() => {
    const error = this.details.error();
    return error ? (error instanceof ApiError ? error.message : 'Something went wrong.') : null;
  });

  protected rulesLabel(count: number): string {
    if (count === 0) {
      return 'accepts every Release';
    }
    return count === 1
      ? '1 rule for which Releases join'
      : `${count} rules for which Releases join`;
  }

  protected kindLabel(kind: ArtifactKind): string {
    return kinds.find((k) => k.value === kind)?.label ?? kind;
  }

  // Adding an artifact.
  protected readonly artifactStatus = new RequestStatus();
  protected readonly addingArtifact = signal(false);
  private readonly artifactModel = signal({
    name: '',
    artifact_kind: 'container_image' as ArtifactKind,
    artifact_name: '',
    required: true,
  });
  protected readonly artifactForm = form(this.artifactModel, (path) => {
    required(path.name, { message: 'Name the artifact, such as api or chart.' });
    required(path.artifact_name, { message: 'Give where its Builds are published.' });
  });

  protected async addArtifact(event: Event): Promise<void> {
    event.preventDefault();
    await submit(this.artifactForm, async () => {
      const artifact = this.artifactModel();
      const added = await this.artifactStatus.run(() =>
        this.api.createArtifact(this.id(), {
          ...artifact,
          name: artifact.name.trim(),
          artifact_name: artifact.artifact_name.trim(),
        }),
      );
      if (added) {
        this.addingArtifact.set(false);
        this.artifactForm().reset({
          name: '',
          artifact_kind: 'container_image',
          artifact_name: '',
          required: true,
        });
        this.details.reload();
      }
      return undefined;
    });
  }

  // Placing on a Target.
  protected readonly placementStatus = new RequestStatus();
  protected readonly targetToPlace = signal('');

  protected async place(event: Event): Promise<void> {
    event.preventDefault();
    const targetId = this.targetToPlace() || this.unplacedTargets()[0]?.id;
    if (!targetId) {
      return;
    }
    if (await this.placementStatus.run(() => this.api.createPlacement(this.id(), targetId))) {
      this.targetToPlace.set('');
      this.details.reload();
    }
  }
}
