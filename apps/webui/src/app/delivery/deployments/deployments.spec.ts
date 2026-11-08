import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { Type } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideRouter, Router } from '@angular/router';
import { API_BASE_URL } from '../../api-base-url';
import { DeployPage } from './deploy-page';
import { DeploymentList } from './deployment-list';
import { DeploymentPage, refreshMs } from './deployment-page';

const v1 = 'https://cp.test/v1';
const ok = <T>(data: T) => ({ status: 'success', data });
const page = <T>(items: T[]) =>
  ok({ items, total_count: items.length, has_next: false, next_cursor: null });

const application = {
  id: 'app_1',
  workspace_id: 'wsp_1',
  project_id: 'prj_1',
  owner_team_id: 'team_1',
  slug: 'payments-api',
  display_name: 'payments-api',
};
const project = { id: 'prj_1', workspace_id: 'wsp_1', display_name: 'Payments', slug: 'payments' };
const environments = [
  { id: 'env_prd', display_name: 'Production', tier: 'production', execution_enabled: true },
  { id: 'env_stg', display_name: 'Staging', tier: 'staging', execution_enabled: true },
];
const targets = {
  env_stg: [
    {
      id: 'tgt_stg',
      environment_id: 'env_stg',
      display_name: 'staging-eu-1',
      agent_principal_id: 'prn_agent',
      locator: {},
    },
  ],
  env_prd: [
    {
      id: 'tgt_prd',
      environment_id: 'env_prd',
      display_name: 'prod-eu-1',
      agent_principal_id: null,
      locator: {},
    },
  ],
};
const lifecycle = {
  id: 'lc_std',
  display_name: 'Standard',
  phases: [
    { id: 'ph_1', name: 'Staging', position: 0, environment_ids: ['env_stg'] },
    { id: 'ph_2', name: 'Production', position: 1, environment_ids: ['env_prd'] },
  ],
};
const channel = { id: 'ch_main', display_name: 'Mainline', lifecycle_id: 'lc_std', rules: [] };
const build = (version: string, digest: string) => ({
  id: 'bld_' + version,
  artifact_name: 'ghcr.io/northwind/payments-api',
  digest,
  version,
});
const release = (overrides: Record<string, unknown> = {}) => ({
  id: 'rel_2',
  application_id: 'app_1',
  channel_id: 'ch_main',
  version: '1.5.0',
  status: 'published',
  builds: [],
  ...overrides,
});
const placement = (target_id: string, current_release_id: string | null) => ({
  application_id: 'app_1',
  target_id,
  source: 'configured',
  apply_config: { definition_artifact: 'chart' },
  current_release_id,
});
const checks = (failing?: { name: string; reason: string }) => [
  { name: 'placement', ok: true, reason: '' },
  failing
    ? { name: failing.name, ok: false, reason: failing.reason }
    : { name: 'agent_assigned', ok: true, reason: '' },
];
const deployment = (overrides: Record<string, unknown> = {}) => ({
  id: 'dep_1',
  application_id: 'app_1',
  release_id: 'rel_2',
  environment_id: 'env_stg',
  target_id: 'tgt_stg',
  status: 'applying',
  source: 'executed',
  reason: 'rollout',
  findings: [],
  requested_at: '2026-11-07T10:00:00Z',
  started_at: '2026-11-07T10:00:00Z',
  finished_at: null,
  session_id: null,
  ...overrides,
});

describe('deployments', () => {
  let http: HttpTestingController;
  let navigate: ReturnType<typeof vi.spyOn>;

  function render<T>(component: Type<T>, inputs: Record<string, unknown>) {
    TestBed.configureTestingModule({
      imports: [component],
      providers: [
        provideHttpClient(),
        provideHttpClientTesting(),
        provideRouter([]),
        { provide: API_BASE_URL, useValue: 'https://cp.test' },
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

  const button = (el: HTMLElement, text: string) =>
    [...el.querySelectorAll('button')].find((b) => b.textContent?.includes(text));

  afterEach(() => {
    vi.useRealTimers();
    http.verify();
  });

  describe('the Deploy page', () => {
    async function renderDeploy(releaseOverrides: Record<string, unknown> = {}) {
      const { fixture, el } = render(DeployPage, { id: 'rel_2' });
      await settle(fixture);
      http.expectOne(`${v1}/releases/rel_2`).flush(ok(release(releaseOverrides)));
      await settle(fixture);
      http.expectOne(`${v1}/applications/app_1`).flush(ok(application));
      await settle(fixture);
      http.expectOne(`${v1}/projects/prj_1`).flush(ok(project));
      http
        .expectOne(`${v1}/applications/app_1/placements`)
        .flush(ok([placement('tgt_stg', 'rel_1'), placement('tgt_prd', null)]));
      http
        .expectOne((r) => r.url.startsWith(`${v1}/workspaces/wsp_1/environments`))
        .flush(page(environments));
      http.expectOne(`${v1}/applications/app_1/channels`).flush(ok([channel]));
      http.expectOne(`${v1}/workspaces/wsp_1/lifecycles`).flush(page([lifecycle]));
      http
        .expectOne(`${v1}/applications/app_1/releases?limit=100&sort_by=created_at&sort_order=desc`)
        .flush(page([release(), release({ id: 'rel_1', version: '1.4.0' })]));
      await settle(fixture);
      http
        .expectOne((r) => r.url.startsWith(`${v1}/environments/env_prd/targets`))
        .flush(page(targets.env_prd));
      http
        .expectOne((r) => r.url.startsWith(`${v1}/environments/env_stg/targets`))
        .flush(page(targets.env_stg));
      await settle(fixture);
      return { fixture, el };
    }

    async function choose(fixture: ComponentFixture<unknown>, el: HTMLElement, name: string) {
      const label = [...el.querySelectorAll('fieldset label')].find((l) =>
        l.textContent?.includes(name),
      )!;
      label.querySelector('input')!.click();
      await settle(fixture);
    }

    function changes(targetId: string, failing?: { name: string; reason: string }) {
      return ok({
        release_id: 'rel_2',
        target_id: targetId,
        current_release_id: 'rel_1',
        artifacts: [
          {
            artifact_id: 'art_api',
            artifact_name: 'api',
            previous: build('1.4.0', 'sha256:aaa'),
            next: build('1.5.0', 'sha256:bbb'),
            commit_shas: ['bbbbbbbb'],
            unresolved: [],
          },
        ],
        checks: checks(failing),
        changes: [
          {
            commit_sha: 'bbbbbbbb',
            source_id: 'src_1',
            title: 'Add checkout',
            message: 'Add checkout',
            author_name: 'Ada',
            author_email: 'ada@example.com',
            committed_at: '2026-11-08T10:00:00Z',
            url: null,
            session_id: null,
            work_item_id: null,
          },
        ],
      });
    }

    it("lists the Application's Targets in the order of the Release's Lifecycle", async () => {
      const { el } = await renderDeploy();

      expect(el.querySelector('h1')?.textContent).toContain('Deploy 1.5.0');
      const groups = [...el.querySelectorAll('fieldset > div')].map((g) =>
        g.textContent?.replace(/\s+/g, ' ').trim(),
      );
      expect(groups[0]).toContain('Staging');
      expect(groups[0]).toContain('staging-eu-1');
      expect(groups[0]).toContain('Runs 1.4.0');
      expect(groups[1]).toContain('prod-eu-1');
      expect(groups[1]).toContain('Nothing deployed yet · no runtime agent');
      expect(button(el, 'Deploy 1.5.0')?.disabled).toBe(true);
    });

    it('shows what changes on a Target and deploys there', async () => {
      const { fixture, el } = await renderDeploy();

      await choose(fixture, el, 'staging-eu-1');
      http.expectOne(`${v1}/releases/rel_2/changes?target_id=tgt_stg`).flush(changes('tgt_stg'));
      await settle(fixture);

      const preview = el.querySelector('article[aria-label="staging-eu-1"]')!;
      const cells = [...preview.querySelectorAll('tbody tr > *')].map((c) => c.textContent?.trim());
      expect(cells).toEqual(['api', '1.4.0', '1.5.0']);
      expect(preview.textContent).toContain('Add checkout');
      expect(preview.textContent).toContain('The Application is placed on the Target');
      button(el, 'Deploy 1.5.0')!.click();
      await settle(fixture);

      const post = http.expectOne(`${v1}/releases/rel_2/deployments`);
      expect(post.request.body).toEqual({ target_ids: ['tgt_stg'] });
      post.flush(ok([deployment()]));
      await settle(fixture);
      expect(navigate).toHaveBeenCalledWith(['/deployments', 'dep_1']);
    });

    it('holds back while a chosen Target fails a check, and says why', async () => {
      const { fixture, el } = await renderDeploy();

      await choose(fixture, el, 'prod-eu-1');
      http.expectOne(`${v1}/releases/rel_2/changes?target_id=tgt_prd`).flush(
        changes('tgt_prd', {
          name: 'agent_assigned',
          reason: 'the target has no agent assigned',
        }),
      );
      await settle(fixture);

      expect(el.textContent).toContain('Failed: A runtime agent is assigned to the Target');
      expect(el.textContent).toContain('the target has no agent assigned');
      expect(button(el, 'Deploy 1.5.0')?.disabled).toBe(true);
      expect(el.textContent).toContain('Every chosen Target must pass its checks.');
    });

    it('deploys to several Targets and lists what started', async () => {
      const { fixture, el } = await renderDeploy();

      await choose(fixture, el, 'staging-eu-1');
      http.expectOne(`${v1}/releases/rel_2/changes?target_id=tgt_stg`).flush(changes('tgt_stg'));
      await choose(fixture, el, 'prod-eu-1');
      http.expectOne(`${v1}/releases/rel_2/changes?target_id=tgt_prd`).flush(changes('tgt_prd'));
      await settle(fixture);
      button(el, 'Deploy to 2 Targets')!.click();
      await settle(fixture);

      const post = http.expectOne(`${v1}/releases/rel_2/deployments`);
      expect(post.request.body).toEqual({ target_ids: ['tgt_stg', 'tgt_prd'] });
      post.flush(
        ok([
          deployment(),
          deployment({ id: 'dep_2', target_id: 'tgt_prd', status: 'awaiting_approval' }),
        ]),
      );
      await settle(fixture);

      const started = el.querySelector('[role="status"]')!;
      expect(started.textContent).toContain('1.5.0 is on its way to 2 Targets');
      expect(started.textContent).toContain('Awaiting approval');
      expect(started.querySelector('a[href="/deployments/dep_2"]')?.textContent).toContain(
        'prod-eu-1',
      );
      expect(navigate).not.toHaveBeenCalled();
    });

    it('only deploys a published Release', async () => {
      const { el } = await renderDeploy({ status: 'draft' });

      expect(el.querySelector('[role="alert"]')?.textContent).toContain(
        'Only a published Release can be deployed',
      );
      expect(el.querySelector('fieldset')).toBeNull();
    });
  });

  describe('the Deployment page', () => {
    async function renderDeployment(
      fixtureDeployment = deployment(),
      events: unknown[] = [],
      flushAll = true,
    ) {
      const rendered = render(DeploymentPage, { id: 'dep_1' });
      await settle(rendered.fixture);
      if (flushAll) {
        await answer(rendered.fixture, fixtureDeployment, events);
      }
      return rendered;
    }

    async function answer(
      fixture: ComponentFixture<unknown>,
      fixtureDeployment: ReturnType<typeof deployment>,
      events: unknown[] = [],
    ) {
      http
        .expectOne(`${v1}/deployments/dep_1`)
        .flush(ok({ deployment: fixtureDeployment, events }));
      await settle(fixture);
      http.expectOne(`${v1}/releases/rel_2`).flush(ok(release()));
      http.expectOne(`${v1}/applications/app_1`).flush(ok(application));
      http.expectOne(`${v1}/environments/env_stg`).flush(ok(environments[1]));
      http
        .expectOne((r) => r.url.startsWith(`${v1}/environments/env_stg/targets`))
        .flush(page(targets.env_stg));
      await settle(fixture);
      http.expectOne(`${v1}/projects/prj_1`).flush(ok(project));
      await settle(fixture);
    }

    it('shows the Release, Target, status and timeline, newest first', async () => {
      const { el } = await renderDeployment(deployment({ status: 'healthy' }), [
        {
          id: 'ev_1',
          kind: 'status_changed',
          status: 'applying',
          occurred_at: '2026-11-07T10:00:00Z',
          detail: null,
        },
        {
          id: 'ev_2',
          kind: 'progress',
          status: null,
          occurred_at: '2026-11-07T10:01:00Z',
          detail: { message: '2 of 3 pods ready' },
        },
        {
          id: 'ev_3',
          kind: 'status_changed',
          status: 'healthy',
          occurred_at: '2026-11-07T10:02:00Z',
          detail: null,
        },
      ]);

      expect(el.querySelector('h1')?.textContent).toContain('1.5.0 to staging-eu-1');
      expect(el.textContent).toContain('In Staging');
      expect(el.querySelector('a[href="/releases/rel_2"]')).not.toBeNull();
      const timeline = [
        ...el.querySelectorAll('section[aria-labelledby="timeline-heading"] li'),
      ].map((li) => li.querySelector('span.text-sm')?.textContent?.trim());
      expect(timeline).toEqual(['Healthy', '2 of 3 pods ready', 'Applying']);
      expect(button(el, 'Cancel deployment')).toBeUndefined();
    });

    it('cancels a Deployment waiting for approval once confirmed', async () => {
      const { fixture, el } = await renderDeployment(
        deployment({ status: 'awaiting_approval', started_at: null }),
      );
      expect(el.textContent).toContain('Waiting for approval');

      button(el, 'Cancel deployment')!.click();
      await settle(fixture);
      expect(el.querySelector('[role="alertdialog"]')?.textContent).toContain(
        "1.5.0 won't be applied to staging-eu-1",
      );
      [...el.querySelectorAll('[role="alertdialog"] button')]
        .find((b) => b.textContent?.includes('Cancel deployment'))!
        .dispatchEvent(new Event('click'));
      await settle(fixture);
      http
        .expectOne(`${v1}/deployments/dep_1/cancel`)
        .flush(ok({ deployment: deployment({ status: 'cancelled' }), events: [] }));
      await settle(fixture);
      await answer(fixture, deployment({ status: 'cancelled' }));

      expect(el.textContent).toContain('Cancelled');
      expect(el.querySelector('[role="alertdialog"]')).toBeNull();
      expect(button(el, 'Cancel deployment')).toBeUndefined();
    });

    it('looks again while the Deployment is still moving', async () => {
      const { fixture } = await renderDeployment(deployment({ status: 'applying' }));

      await new Promise((resolve) => setTimeout(resolve, refreshMs + 50));
      await answer(fixture, deployment({ status: 'healthy' }));

      // Healthy no longer moves by itself: nothing more is asked for.
      await new Promise((resolve) => setTimeout(resolve, refreshMs + 50));
      http.expectNone(`${v1}/deployments/dep_1`);
    }, 15000);
  });

  describe("the Application's Deployments", () => {
    it('lists them with their Release, Target and status', async () => {
      const { fixture, el } = render(DeploymentList, {
        applicationId: 'app_1',
        targets: [...targets.env_stg, ...targets.env_prd],
        environments,
      });
      await settle(fixture);
      http
        .expectOne(
          `${v1}/applications/app_1/deployments?limit=20&sort_by=requested_at&sort_order=desc`,
        )
        .flush(page([deployment({ status: 'healthy' })]));
      await settle(fixture);
      http.expectOne(`${v1}/releases/rel_2`).flush(ok(release()));
      await settle(fixture);

      const row = el.querySelector('tbody tr')!;
      expect(row.querySelector('a')?.textContent?.trim()).toBe('1.5.0 to staging-eu-1');
      expect(row.querySelector('a')?.getAttribute('href')).toBe('/deployments/dep_1');
      expect(row.textContent).toContain('Healthy');
      expect(el.textContent).toContain('1 Deployment');
    });
  });
});
