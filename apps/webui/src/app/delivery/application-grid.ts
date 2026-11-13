import { Component, input } from '@angular/core';
import { RouterLink } from '@angular/router';
import {
  Application,
  DeploymentStatus,
  Environment,
  RunsOnApplication,
  WorkspaceRunsOn,
} from '../api/models';
import { DeploymentStatusBadge } from './deployments/deployment-status';

/** A Target an Application is placed on, and what runs there now. */
export interface GridPlacement {
  target: string;
  version: string | null;
  status: DeploymentStatus | null;
}

/** One Application's row: per Environment, in column order, the Targets it's placed on. */
export interface GridRow {
  application: Application;
  /** The owning Team's name, shown only when it differs from the Project's. */
  team: string;
  cells: GridPlacement[][];
}

/** The part of a Project a grid heading needs. */
export interface GridProject {
  id: string;
  display_name: string;
}

/** A Project's rows, under a heading that links to it. */
export interface GridGroup {
  project: GridProject;
  team: string;
  rows: GridRow[];
}

/** Groups a "what runs where" read into the grid's rows, one group per Project. */
export function runsOnGroups(page: WorkspaceRunsOn): GridGroup[] {
  const groups = new Map<string, GridGroup>();
  for (const item of page.items) {
    let group = groups.get(item.project.id);
    if (!group) {
      group = { project: item.project, team: item.project.owner_team.display_name, rows: [] };
      groups.set(item.project.id, group);
    }
    group.rows.push(applicationRow(item, page.environments));
  }
  return [...groups.values()].sort((a, b) =>
    a.project.display_name.localeCompare(b.project.display_name),
  );
}

function applicationRow(item: RunsOnApplication, environments: Environment[]): GridRow {
  return {
    application: item.application,
    team: item.owner_team.id === item.project.owner_team.id ? '' : item.owner_team.display_name,
    cells: environments.map((environment) =>
      item.targets
        .filter((target) => target.environment_id === environment.id)
        .map((target) => ({
          target: target.target_display_name,
          version: target.release_version,
          status: target.deployment_status,
        })),
    ),
  };
}

/**
 * Applications by Environment: the Targets each is placed on. With several groups, each Project's
 * Applications sit under a row naming it.
 */
@Component({
  selector: 'app-application-grid',
  imports: [RouterLink, DeploymentStatusBadge],
  template: `
    <div class="border-border relative overflow-x-auto border">
      <table class="w-full min-w-225 table-fixed border-collapse text-sm">
        <caption class="sr-only">
          {{
            caption()
          }}
        </caption>
        <thead>
          <tr class="text-left">
            <th
              scope="col"
              class="border-border text-muted-foreground w-[28%] border-b px-4 py-3 font-semibold"
            >
              Application
            </th>
            @for (environment of environments(); track environment.id) {
              <th
                scope="col"
                class="border-border text-heading border-b border-l px-4 py-3 font-semibold"
              >
                {{ environment.display_name }}
              </th>
            }
          </tr>
        </thead>
        @for (group of groups(); track group.project.id) {
          <tbody>
            @if (showProjects()) {
              <tr class="bg-muted/40">
                <th
                  scope="rowgroup"
                  class="border-border border-b px-4 py-2.5 text-left font-normal"
                  [attr.colspan]="environments().length + 1"
                >
                  <span class="flex flex-wrap items-center justify-between gap-x-4 gap-y-1">
                    <span class="flex flex-wrap items-baseline gap-x-2">
                      <a
                        class="text-heading font-semibold no-underline hover:underline"
                        [routerLink]="['/projects', group.project.id]"
                        >{{ group.project.display_name }}</a
                      >
                      @if (group.team) {
                        <span class="text-muted-foreground text-[13px]"
                          >Owned by {{ group.team }}</span
                        >
                      }
                    </span>
                    <a
                      class="text-[13px] font-semibold"
                      routerLink="/applications/new"
                      [queryParams]="{ project: group.project.id }"
                      [attr.aria-label]="'New application in ' + group.project.display_name"
                      >New application</a
                    >
                  </span>
                </th>
              </tr>
            }
            @for (row of group.rows; track row.application.id) {
              <tr>
                <th
                  scope="row"
                  class="border-border border-b px-4 py-4 text-left align-top font-normal"
                  [class.pl-8]="showProjects()"
                >
                  <a
                    [routerLink]="['/applications', row.application.id]"
                    class="text-heading text-[15px] font-semibold no-underline hover:underline"
                  >
                    {{ row.application.display_name }}
                  </a>
                  @if (row.team) {
                    <div class="text-muted-foreground text-[13px]">Owned by {{ row.team }}</div>
                  }
                </th>
                @for (cell of row.cells; track $index) {
                  <td class="border-border border-b border-l px-4 py-4 align-top">
                    @if (cell.length > 0) {
                      <ul class="flex flex-col gap-2">
                        @for (placed of cell; track placed.target) {
                          <li class="flex flex-col">
                            @if (placed.version) {
                              <span class="text-heading font-semibold tabular-nums">{{
                                placed.version
                              }}</span>
                            } @else {
                              <span class="text-muted-foreground text-[13px]"
                                >Nothing deployed</span
                              >
                            }
                            <span class="text-muted-foreground text-[13px]">{{
                              placed.target
                            }}</span>
                            @if (placed.status) {
                              <app-deployment-status class="mt-1" [status]="placed.status" />
                            }
                          </li>
                        }
                      </ul>
                    } @else {
                      <span class="text-muted-foreground text-[13px]">Not placed</span>
                    }
                  </td>
                }
              </tr>
            } @empty {
              <tr>
                <td
                  class="border-border text-muted-foreground border-b px-4 py-3 text-[13px]"
                  [class.pl-8]="showProjects()"
                  [attr.colspan]="environments().length + 1"
                >
                  No Applications in this Project yet.
                </td>
              </tr>
            }
          </tbody>
        }
      </table>
    </div>
  `,
})
export class ApplicationGrid {
  readonly environments = input.required<Environment[]>();
  readonly groups = input.required<GridGroup[]>();
  /** Whether each group gets a row naming its Project. */
  readonly showProjects = input(true);
  readonly caption = input('Where each Application is placed, by Environment');
}
