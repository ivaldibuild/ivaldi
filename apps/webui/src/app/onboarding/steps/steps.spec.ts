import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { signal, Type } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideRouter, Router } from '@angular/router';
import { CurrentUser } from '../../api/current-user';
import { API_BASE_URL } from '../../api-base-url';
import { Session } from '../../auth/session';
import { ClusterStep } from './cluster-step';
import { EnvironmentsStep } from './environments-step';
import { OrganisationStep } from './organisation-step';
import { TeamStep } from './team-step';

const v1 = 'https://cp.test/v1';
const ok = <T>(data: T) => ({ status: 'success', data });
const page = <T>(items: T[]) => ok({ items, total_count: items.length });
const organization = {
  id: 'org_1',
  slug: 'northwind',
  name: 'Northwind Labs Ltd',
  display_name: 'Northwind',
  logo_url: null,
};

/** A stand-in for CurrentUser: Ama, an owner unless a test says otherwise. */
function fakeUser(canManage = true) {
  return {
    displayName: signal('Ama Mensah'),
    firstName: signal('Ama'),
    canManageOrganization: signal(canManage),
    reload: vi.fn(),
  };
}

const workspace = { id: 'wsp_1', slug: 'default', display_name: 'Default', is_default: true };

describe('setup steps', () => {
  let http: HttpTestingController;
  let navigate: ReturnType<typeof vi.spyOn>;

  let user: ReturnType<typeof fakeUser>;

  async function render<T>(
    component: Type<T>,
    canManage = true,
  ): Promise<{ fixture: ComponentFixture<T>; el: HTMLElement }> {
    user = fakeUser(canManage);
    TestBed.configureTestingModule({
      imports: [component],
      providers: [
        provideHttpClient(),
        provideHttpClientTesting(),
        provideRouter([]),
        { provide: API_BASE_URL, useValue: 'https://cp.test' },
        { provide: Session, useValue: { principalId: () => 'prn_me' } },
        { provide: CurrentUser, useValue: user },
      ],
    });
    http = TestBed.inject(HttpTestingController);
    navigate = vi.spyOn(TestBed.inject(Router), 'navigate').mockResolvedValue(true);
    const fixture = TestBed.createComponent(component);
    fixture.detectChanges();
    return { fixture, el: fixture.nativeElement as HTMLElement };
  }

  async function settle(fixture: ComponentFixture<unknown>): Promise<void> {
    for (let i = 0; i < 3; i++) {
      await new Promise((resolve) => setTimeout(resolve));
    }
    await fixture.whenStable();
    fixture.detectChanges();
  }

  function type(el: HTMLElement, selector: string, value: string): void {
    const input = el.querySelector<HTMLInputElement>(selector)!;
    input.value = value;
    input.dispatchEvent(new Event('input'));
  }

  function submit(el: HTMLElement, formId: string): void {
    el.querySelector(`#${formId}`)!.dispatchEvent(new Event('submit'));
  }

  afterEach(() => http.verify());

  async function renderOrganisation(canManage = true) {
    const rendered = await render(OrganisationStep, canManage);
    http.expectOne(`${v1}/organization`).flush(ok(organization));
    http.expectOne(`${v1}/workspaces`).flush(ok([workspace]));
    await settle(rendered.fixture);
    return rendered;
  }

  it('greets the person and fills in the Organisation and its Workspace', async () => {
    const { el } = await renderOrganisation();

    expect(el.querySelector('h1')?.textContent).toContain('Welcome to Ivaldi, Ama');
    expect(el.querySelector<HTMLInputElement>('#org-display-name')?.value).toBe('Northwind');
    expect(el.querySelector<HTMLInputElement>('#org-name')?.value).toBe('Northwind Labs Ltd');
    expect(el.querySelector<HTMLInputElement>('#org-slug')?.value).toBe('northwind');
    expect(el.querySelector<HTMLInputElement>('#workspace-name')?.value).toBe('Default');
  });

  it('saves only what changed on the Organisation and the Workspace', async () => {
    const { fixture, el } = await renderOrganisation();

    type(el, '#org-display-name', 'Northwind Labs');
    type(el, '#workspace-name', 'Platform');
    type(el, '#workspace-slug', 'platform');
    submit(el, 'organisation-form');
    await settle(fixture);

    const org = http.expectOne(`${v1}/organization`);
    expect(org.request.method).toBe('PATCH');
    expect(org.request.body).toEqual({ display_name: 'Northwind Labs' });
    org.flush(ok({ ...organization, display_name: 'Northwind Labs' }));
    await settle(fixture);
    expect(user.reload).toHaveBeenCalled();

    const ws = http.expectOne(`${v1}/workspaces/wsp_1`);
    expect(ws.request.method).toBe('PATCH');
    expect(ws.request.body).toEqual({ display_name: 'Platform', slug: 'platform' });
    ws.flush(ok({ ...workspace, display_name: 'Platform', slug: 'platform' }));
    await settle(fixture);
    expect(navigate).toHaveBeenCalledWith(['/setup', 'sign-in']);
  });

  it('warns that a new address breaks links that name the old one', async () => {
    const { fixture, el } = await renderOrganisation();

    type(el, '#org-slug', 'northwind-labs');
    fixture.detectChanges();

    expect(el.textContent).toContain(
      'Links and sign-in hints that name northwind will stop working.',
    );
  });

  it('shows the Organisation read-only to people who cannot manage it', async () => {
    const { fixture, el } = await renderOrganisation(false);

    expect(el.querySelector('#org-display-name')).toBeNull();
    expect(el.textContent).toContain('Only owners and admins can change these.');

    submit(el, 'organisation-form');
    await settle(fixture);
    http.expectNone((r) => r.method === 'PATCH');
    expect(navigate).toHaveBeenCalledWith(['/setup', 'sign-in']);
  });

  it('names the person setting up as the Team lead', async () => {
    const { fixture, el } = await render(TeamStep);
    http.expectOne(`${v1}/teams`).flush(page([]));
    await settle(fixture);

    expect(el.textContent?.replace(/\s+/g, ' ')).toContain('Ama Mensah (you)');
  });

  it('creates the Team with you as lead and its agents', async () => {
    const { fixture, el } = await render(TeamStep);
    http.expectOne(`${v1}/teams`).flush(page([]));
    await settle(fixture);

    type(el, '#team-name', 'Payments');
    type(el, '#agent-name', 'Release assistant');
    fixture.detectChanges();
    el.querySelector<HTMLButtonElement>('button[type="button"]:not([aria-label])')!.click();
    submit(el, 'team-form');
    await settle(fixture);

    const create = http.expectOne((r) => r.method === 'POST' && r.url === `${v1}/teams`);
    expect(create.request.body).toEqual({ display_name: 'Payments' });
    create.flush(ok({ id: 'team_1', slug: 'payments', display_name: 'Payments', members: [] }));
    await settle(fixture);

    const lead = http.expectOne(`${v1}/teams/team_1/members/prn_me`);
    expect(lead.request.method).toBe('PUT');
    expect(lead.request.body).toEqual({ role: 'lead' });
    lead.flush(ok([]));
    await settle(fixture);

    const agent = http.expectOne(`${v1}/teams/team_1/agents`);
    expect(agent.request.body).toEqual({ display_name: 'Release assistant', role: 'member' });
    agent.flush(ok({ agent: { id: 'prn_agent', display_name: 'Release assistant' } }));
    await settle(fixture);

    expect(navigate).toHaveBeenCalledWith(['/setup', 'environments']);
  });

  it('shows the control plane’s reason when the Team is refused', async () => {
    const { fixture, el } = await render(TeamStep);
    http.expectOne(`${v1}/teams`).flush(page([]));
    await settle(fixture);

    type(el, '#team-name', 'Payments');
    submit(el, 'team-form');
    await settle(fixture);
    http
      .expectOne((r) => r.method === 'POST' && r.url === `${v1}/teams`)
      .flush(
        { title: 'Conflict', detail: 'a team with this slug already exists' },
        { status: 409, statusText: 'Conflict' },
      );
    await settle(fixture);

    expect(el.querySelector('[role="alert"]')?.textContent).toContain(
      'A team with this slug already exists.',
    );
    expect(navigate).not.toHaveBeenCalled();
  });

  it('creates the starter Environments, then the Standard and Hotfix Lifecycles', async () => {
    const { fixture, el } = await render(EnvironmentsStep);
    http.expectOne(`${v1}/workspaces`).flush(ok([workspace]));
    await settle(fixture);
    http.expectOne(`${v1}/workspaces/wsp_1/environments`).flush(page([]));
    http.expectOne(`${v1}/workspaces/wsp_1/lifecycles`).flush(page([]));
    await settle(fixture);

    submit(el, 'environments-form');
    for (const [i, name] of ['Development', 'Staging', 'Production'].entries()) {
      await settle(fixture);
      const req = http.expectOne(
        (r) => r.method === 'POST' && r.url === `${v1}/workspaces/wsp_1/environments`,
      );
      expect(req.request.body).toEqual({
        display_name: name,
        tier: name.toLowerCase(),
        execution_enabled: true,
      });
      req.flush(ok({ id: `env_${i}`, display_name: name, tier: name.toLowerCase() }));
    }
    await settle(fixture);

    const standard = http.expectOne(
      (r) => r.method === 'POST' && r.url === `${v1}/workspaces/wsp_1/lifecycles`,
    );
    expect(standard.request.body.phases).toEqual([
      { name: 'Development', environment_ids: ['env_0'] },
      { name: 'Staging', environment_ids: ['env_1'] },
      { name: 'Production', environment_ids: ['env_2'] },
    ]);
    standard.flush(ok({ id: 'lc_1', phases: [] }));
    await settle(fixture);

    const hotfix = http.expectOne(
      (r) => r.method === 'POST' && r.url === `${v1}/workspaces/wsp_1/lifecycles`,
    );
    expect(hotfix.request.body.display_name).toBe('Hotfix');
    expect(hotfix.request.body.phases).toEqual([
      { name: 'Staging', environment_ids: ['env_1'] },
      { name: 'Production', environment_ids: ['env_2'] },
    ]);
    hotfix.flush(ok({ id: 'lc_2', phases: [] }));
    await settle(fixture);

    expect(navigate).toHaveBeenCalledWith(['/setup', 'cluster']);
  });

  it('records a Kubernetes Target for an Environment', async () => {
    const { fixture, el } = await render(ClusterStep);
    http.expectOne(`${v1}/workspaces`).flush(ok([workspace]));
    await settle(fixture);
    http.expectOne(`${v1}/workspaces/wsp_1/environments`).flush(
      page([
        { id: 'env_dev', display_name: 'Development', tier: 'development' },
        { id: 'env_stg', display_name: 'Staging', tier: 'staging' },
      ]),
    );
    await settle(fixture);

    type(el, '#target-name', 'staging-eu-1');
    type(el, '#target-cluster', 'eu-west-1-main');
    type(el, '#target-namespace', 'payments');
    submit(el, 'target-form');
    await settle(fixture);

    const req = http.expectOne(`${v1}/environments/env_stg/targets`);
    expect(req.request.body).toEqual({
      display_name: 'staging-eu-1',
      runtime: 'kubernetes',
      locator: { cluster: 'eu-west-1-main', namespace: 'payments' },
    });
    req.flush(
      ok({
        id: 'tgt_1',
        environment_id: 'env_stg',
        display_name: 'staging-eu-1',
        locator: { cluster: 'eu-west-1-main', namespace: 'payments' },
      }),
    );
    await settle(fixture);

    expect(el.textContent).toContain('staging-eu-1 is ready for an agent');
  });

  it('checks the namespace is a Kubernetes name before calling the control plane', async () => {
    const { fixture, el } = await render(ClusterStep);
    http.expectOne(`${v1}/workspaces`).flush(ok([workspace]));
    await settle(fixture);
    http
      .expectOne(`${v1}/workspaces/wsp_1/environments`)
      .flush(page([{ id: 'env_stg', display_name: 'Staging', tier: 'staging' }]));
    await settle(fixture);

    type(el, '#target-name', 'staging-eu-1');
    type(el, '#target-cluster', 'main');
    type(el, '#target-namespace', 'Payments Team');
    submit(el, 'target-form');
    await settle(fixture);

    expect(el.textContent).toContain('Use a Kubernetes namespace');
    http.expectNone(`${v1}/environments/env_stg/targets`);
  });
});
