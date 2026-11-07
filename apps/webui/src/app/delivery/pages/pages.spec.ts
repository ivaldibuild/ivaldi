import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { signal, Type } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideRouter, Router } from '@angular/router';
import { API_BASE_URL } from '../../api-base-url';
import { WorkspaceContext } from '../workspace-context';
import { ApplicationPage } from './application-page';
import { ApplicationsPage } from './applications-page';
import { NewApplicationPage } from './new-application-page';

const v1 = 'https://cp.test/v1';
const ok = <T>(data: T) => ({ status: 'success', data });
const page = <T>(items: T[]) =>
  ok({ items, total_count: items.length, has_next: false, next_cursor: null });
/** The Workspace's Projects, as labels and pickers ask for them. */
const projectList = `${v1}/workspaces/wsp_1/projects?limit=100&sort_by=display_name`;

const workspace = { id: 'wsp_1', slug: 'platform', display_name: 'Platform', is_default: true };
const team = { id: 'team_1', slug: 'payments', display_name: 'Payments', members: [] };
const project = {
  id: 'prj_1',
  workspace_id: 'wsp_1',
  owner_team_id: 'team_1',
  slug: 'payments',
  display_name: 'Payments',
};
const environments = [
  {
    id: 'env_stg',
    slug: 'staging',
    display_name: 'Staging',
    tier: 'staging',
    execution_enabled: true,
  },
  {
    id: 'env_prd',
    slug: 'production',
    display_name: 'Production',
    tier: 'production',
    execution_enabled: true,
  },
];
const targets = {
  env_stg: [
    {
      id: 'tgt_stg',
      environment_id: 'env_stg',
      display_name: 'staging-eu-1',
      locator: { cluster: 'eu', namespace: 'payments' },
    },
  ],
  env_prd: [
    {
      id: 'tgt_prd',
      environment_id: 'env_prd',
      display_name: 'prod-eu-1',
      locator: { cluster: 'eu', namespace: 'payments' },
    },
  ],
};
const application = {
  id: 'app_1',
  workspace_id: 'wsp_1',
  project_id: 'prj_1',
  owner_team_id: 'team_1',
  slug: 'payments-api',
  display_name: 'payments-api',
  source_id: null,
};
const standard = {
  id: 'lc_std',
  slug: 'standard',
  display_name: 'Standard',
  phases: [
    { id: 'ph_1', name: 'Staging', position: 0, environment_ids: ['env_stg'] },
    { id: 'ph_2', name: 'Production', position: 1, environment_ids: ['env_prd'] },
  ],
};

const releasesUrl = '/applications/app_1/releases?limit=20&sort_by=created_at&sort_order=desc';

describe('delivery pages', () => {
  let http: HttpTestingController;
  let navigate: ReturnType<typeof vi.spyOn>;

  function render<T>(
    component: Type<T>,
    inputs: Record<string, unknown> = {},
  ): { fixture: ComponentFixture<T>; el: HTMLElement } {
    TestBed.configureTestingModule({
      imports: [component],
      providers: [
        provideHttpClient(),
        provideHttpClientTesting(),
        provideRouter([]),
        { provide: API_BASE_URL, useValue: 'https://cp.test' },
        {
          provide: WorkspaceContext,
          useValue: { current: signal(workspace), workspaces: signal([workspace]) },
        },
      ],
    });
    http = TestBed.inject(HttpTestingController);
    navigate = vi.spyOn(TestBed.inject(Router), 'navigate').mockResolvedValue(true);
    const fixture = TestBed.createComponent(component);
    for (const [name, value] of Object.entries(inputs)) {
      fixture.componentRef.setInput(name, value);
    }
    fixture.detectChanges();
    return { fixture, el: fixture.nativeElement as HTMLElement };
  }

  async function settle(fixture: ComponentFixture<unknown>): Promise<void> {
    for (let i = 0; i < 4; i++) {
      await new Promise((resolve) => setTimeout(resolve));
      fixture.detectChanges();
    }
  }

  function flushTargets(): void {
    http.expectOne(`${v1}/environments/env_stg/targets`).flush(page(targets.env_stg));
    http.expectOne(`${v1}/environments/env_prd/targets`).flush(page(targets.env_prd));
  }

  /** Answers every outstanding request from the fixtures until the page goes quiet. */
  async function drain(fixture: ComponentFixture<unknown>): Promise<void> {
    for (let round = 0; round < 5; round++) {
      await settle(fixture);
      const pending = http.match(() => true);
      if (pending.length === 0) {
        return;
      }
      for (const req of pending) {
        const url = req.request.url.replace(v1, '');
        const body: Record<string, unknown> = {
          '/applications/app_1': ok(application),
          '/workspaces/wsp_1/projects?limit=100&sort_by=display_name': page([project]),
          '/teams': page([team]),
          '/applications/app_1/channels': ok([]),
          '/applications/app_1/artifacts': ok([]),
          '/applications/app_1/placements': ok([]),
          '/workspaces/wsp_1/lifecycles': page([standard]),
          '/workspaces/wsp_1/environments': page(environments),
          '/environments/env_stg/targets': page(targets.env_stg),
          '/environments/env_prd/targets': page(targets.env_prd),
          [releasesUrl]: page([]),
        };
        req.flush(body[url] ?? ok([]));
      }
    }
  }

  afterEach(() => http.verify());

  it('shows which Targets each Application is placed on, per Environment', async () => {
    const { fixture, el } = render(ApplicationsPage);
    await settle(fixture);
    http.expectOne(`${v1}/workspaces/wsp_1/applications`).flush(page([application]));
    http.expectOne(`${v1}/workspaces/wsp_1/environments`).flush(page(environments));
    http.expectOne(projectList).flush(page([project]));
    http.expectOne(`${v1}/teams`).flush(page([team]));
    await settle(fixture);
    flushTargets();
    http.expectOne(`${v1}/applications/app_1/placements`).flush(
      ok([
        {
          application_id: 'app_1',
          target_id: 'tgt_stg',
          source: 'configured',
          current_release_id: null,
        },
      ]),
    );
    await settle(fixture);

    const headers = [...el.querySelectorAll('thead th')].map((th) => th.textContent?.trim());
    expect(headers).toEqual(['Application', 'Staging', 'Production']);
    const cells = [...el.querySelectorAll('tbody td')].map((td) => td.textContent?.trim());
    expect(cells).toEqual(['staging-eu-1', 'Not placed']);
    const group = el.querySelector('th[scope="rowgroup"]');
    expect(group?.querySelector('a')?.textContent?.trim()).toBe('Payments');
    expect(group?.querySelector('a')?.getAttribute('href')).toBe('/projects/prj_1');
    expect(group?.textContent).toContain('Owned by Payments');
    expect(
      group?.querySelector('a[aria-label="New application in Payments"]')?.getAttribute('href'),
    ).toBe('/applications/new?project=prj_1');
    const row = el.querySelector('th[scope="row"]');
    expect(row?.textContent?.trim()).toBe('payments-api');
  });

  it('starts from the Project it was opened from', async () => {
    const other = { ...project, id: 'prj_2', owner_team_id: 'team_2', display_name: 'Core' };
    const { fixture, el } = render(NewApplicationPage, { project: 'prj_2' });
    await settle(fixture);
    http.expectOne(projectList).flush(page([project, other]));
    http
      .expectOne(`${v1}/teams`)
      .flush(page([team, { ...team, id: 'team_2', display_name: 'Core' }]));
    http.expectOne(`${v1}/workspaces/wsp_1/lifecycles`).flush(page([standard]));
    await settle(fixture);

    expect(el.querySelector<HTMLSelectElement>('#application-project')!.value).toBe('prj_2');
    expect(el.querySelector<HTMLSelectElement>('#application-team')!.value).toBe('team_2');
  });

  it('creates a Project and an Application on its Lifecycle', async () => {
    const { fixture, el } = render(NewApplicationPage);
    await settle(fixture);
    http.expectOne(projectList).flush(page([]));
    http.expectOne(`${v1}/teams`).flush(page([team]));
    http.expectOne(`${v1}/workspaces/wsp_1/lifecycles`).flush(page([standard]));
    await settle(fixture);

    const type = (selector: string, value: string) => {
      const input = el.querySelector<HTMLInputElement>(selector)!;
      input.value = value;
      input.dispatchEvent(new Event('input'));
    };
    type('#application-name', 'payments-api');
    type('#project-name', 'Payments Team');
    el.querySelector('#application-form')!.dispatchEvent(new Event('submit'));
    await settle(fixture);

    const createProject = http.expectOne(`${v1}/workspaces/wsp_1/projects`);
    expect(createProject.request.body).toEqual({
      display_name: 'Payments Team',
      slug: 'payments-team',
      owner_team_id: 'team_1',
    });
    expect(createProject.request.method).toBe('POST');
    createProject.flush(ok({ ...project, id: 'prj_new' }));
    await settle(fixture);

    const createApplication = http.expectOne(`${v1}/projects/prj_new/applications`);
    expect(createApplication.request.body).toEqual({
      display_name: 'payments-api',
      owner_team_id: 'team_1',
      default_lifecycle_id: 'lc_std',
    });
    createApplication.flush(ok({ application, default_channel: {} }));
    await settle(fixture);

    expect(navigate).toHaveBeenCalledWith(['/applications', 'app_1']);
  });

  async function renderApplication() {
    const { fixture, el } = render(ApplicationPage, { id: 'app_1' });
    await settle(fixture);
    http.expectOne(`${v1}/applications/app_1`).flush(ok(application));
    await settle(fixture);
    http.expectOne(projectList).flush(page([project]));
    http.expectOne(`${v1}/teams`).flush(page([team]));
    http.expectOne(`${v1}/applications/app_1/channels`).flush(
      ok([
        {
          id: 'ch_1',
          lifecycle_id: 'lc_std',
          slug: 'default',
          display_name: 'Default',
          description: null,
          is_default: true,
          rules: [],
        },
      ]),
    );
    http.expectOne(`${v1}/applications/app_1/artifacts`).flush(ok([]));
    http.expectOne(`${v1}/applications/app_1/placements`).flush(ok([]));
    http.expectOne(`${v1}/workspaces/wsp_1/lifecycles`).flush(page([standard]));
    http.expectOne(`${v1}/workspaces/wsp_1/environments`).flush(page(environments));
    await settle(fixture);
    flushTargets();
    await settle(fixture);
    http.expectOne(`${v1}${releasesUrl}`).flush(page([]));
    await settle(fixture);
    return { fixture, el };
  }

  it('shows the default Channel and the Lifecycle it follows', async () => {
    const { el } = await renderApplication();

    expect(el.querySelector('h1')?.textContent).toContain('payments-api');
    const crumbs = [...el.querySelectorAll('nav[aria-label="Breadcrumb"] a')];
    expect(crumbs.map((a) => [a.textContent?.trim(), a.getAttribute('href')])).toEqual([
      ['Projects', '/projects'],
      ['Payments', '/projects/prj_1'],
    ]);
    expect(el.textContent).toContain('Follows Standard');
    const phases = [...el.querySelectorAll('ol li span.font-semibold')].map((s) =>
      s.textContent?.trim(),
    );
    expect(phases).toEqual(['Staging', 'Production']);
    expect(el.textContent).toContain('Not placed on any Target yet.');
  });

  it('places the Application on a Target and reloads', async () => {
    const { fixture, el } = await renderApplication();

    const select = el.querySelector<HTMLSelectElement>('#placement-target')!;
    select.value = 'tgt_prd';
    select.dispatchEvent(new Event('change'));
    select.closest('form')!.dispatchEvent(new Event('submit'));
    await settle(fixture);

    const req = http.expectOne(`${v1}/applications/app_1/placements`);
    expect(req.request.method).toBe('POST');
    expect(req.request.body).toEqual({ target_id: 'tgt_prd' });
    req.flush(ok({ application_id: 'app_1', target_id: 'tgt_prd', source: 'configured' }));
    await settle(fixture);

    // The page reloads after a change.
    await drain(fixture);
    expect(el.querySelector('#placement-target')).not.toBeNull();
  });

  it('declares an artifact', async () => {
    const { fixture, el } = await renderApplication();

    [...el.querySelectorAll('button')]
      .find((b) => b.textContent?.includes('Add artifact'))!
      .click();
    await settle(fixture);
    const type = (selector: string, value: string) => {
      const input = el.querySelector<HTMLInputElement>(selector)!;
      input.value = value;
      input.dispatchEvent(new Event('input'));
    };
    type('#artifact-name', 'api');
    type('#artifact-published', 'ghcr.io/northwind/payments-api');
    el.querySelector('#artifact-name')!.closest('form')!.dispatchEvent(new Event('submit'));
    await settle(fixture);

    const req = http.expectOne(`${v1}/applications/app_1/artifacts`);
    expect(req.request.body).toEqual({
      name: 'api',
      artifact_kind: 'container_image',
      artifact_name: 'ghcr.io/northwind/payments-api',
      required: true,
    });
    req.flush(ok({ id: 'art_1', name: 'api' }));
    await drain(fixture);
    expect(el.querySelector('#artifact-name')).toBeNull();
  });
});
