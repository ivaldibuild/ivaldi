import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { API_BASE_URL } from '../api-base-url';
import { ProjectSources } from './project-sources';

const v1 = 'https://cp.test/v1';
const ok = <T>(data: T) => ({ status: 'success', data });
const page = <T>(items: T[]) =>
  ok({ items, total_count: items.length, has_next: false, next_cursor: null });
const sourcesURL = `${v1}/projects/prj_1/sources?sort_by=full_name&limit=100`;

const connection = {
  id: 'vcn_1',
  organization_id: 'org_1',
  provider: 'github',
  host_url: 'https://github.com',
  auth_kind: 'app_installation',
  credential_ref: null,
  external_account_id: '42',
  external_account_name: 'northwind',
  webhook_route_key: 'github:github.com:42',
  lookup_from: 'control_plane',
  lookup_pool: null,
  status: 'active',
  created_by_principal_id: 'prn_ama',
  created_at: '2026-11-08T10:00:00Z',
  updated_at: '2026-11-08T10:00:00Z',
  deleted_at: null,
};

const api = {
  id: 'src_1',
  organization_id: 'org_1',
  connection_id: 'vcn_1',
  workspace_id: 'wsp_1',
  project_id: 'prj_1',
  external_repository_id: '7',
  repository_key: 'github:github.com:7',
  full_name: 'northwind/api',
  default_branch: 'main',
  visibility: 'private',
  clone_url: 'https://github.com/northwind/api.git',
  web_url: 'https://github.com/northwind/api',
  created_by_principal_id: 'prn_ama',
  upstream_archived_at: null,
  created_at: '2026-11-08T10:00:00Z',
  updated_at: '2026-11-08T10:00:00Z',
  archived_at: null,
};

const repositories = [
  {
    external_id: '7',
    full_name: 'northwind/api',
    default_branch: 'main',
    visibility: 'private',
    web_url: '',
    connected: true,
  },
  {
    external_id: '8',
    full_name: 'northwind/web',
    default_branch: 'main',
    visibility: 'public',
    web_url: '',
    connected: false,
  },
  {
    external_id: '9',
    full_name: 'northwind/infra',
    default_branch: 'main',
    visibility: 'private',
    web_url: '',
    connected: true,
  },
];
const permissionsURL = `${v1}/me/permissions?resource_id=prj_1&permission=source.manage`;

describe('ProjectSources', () => {
  let http: HttpTestingController;

  async function render(
    granted = ['source.manage'],
  ): Promise<{ fixture: ComponentFixture<ProjectSources>; el: HTMLElement }> {
    TestBed.configureTestingModule({
      imports: [ProjectSources],
      providers: [
        provideHttpClient(),
        provideHttpClientTesting(),
        provideRouter([]),
        { provide: API_BASE_URL, useValue: 'https://cp.test' },
      ],
    });
    http = TestBed.inject(HttpTestingController);
    const fixture = TestBed.createComponent(ProjectSources);
    fixture.componentRef.setInput('projectId', 'prj_1');
    fixture.detectChanges();
    await settle(fixture);
    http.expectOne(sourcesURL).flush(page([api]));
    http.expectOne(permissionsURL).flush(ok({ resource_id: 'prj_1', granted }));
    await settle(fixture);
    return { fixture, el: fixture.nativeElement as HTMLElement };
  }

  async function settle(fixture: ComponentFixture<unknown>): Promise<void> {
    for (let i = 0; i < 3; i++) {
      await new Promise((resolve) => setTimeout(resolve));
      fixture.detectChanges();
    }
  }

  function button(el: HTMLElement, label: string): HTMLButtonElement {
    return [...el.querySelectorAll('button')].find(
      (b) => b.getAttribute('aria-label') === label || b.textContent?.trim().endsWith(label),
    )!;
  }

  async function openConnect(
    fixture: ComponentFixture<ProjectSources>,
    el: HTMLElement,
    connections: unknown[],
  ): Promise<void> {
    button(el, 'Connect repository').click();
    await settle(fixture);
    http.expectOne(`${v1}/connections`).flush(ok(connections));
    await settle(fixture);
  }

  afterEach(() => http.verify());

  it('lists the Project’s Sources', async () => {
    const { el } = await render();
    expect(el.querySelector('tbody')?.textContent).toContain('northwind/api');
    expect(el.querySelector<HTMLAnchorElement>('tbody a')?.href).toBe(
      'https://github.com/northwind/api',
    );
  });

  it('connects a repository picked from the Connection’s account', async () => {
    const { fixture, el } = await render();
    await openConnect(fixture, el, [connection]);
    http.expectOne(`${v1}/connections/vcn_1/repositories`).flush(ok(repositories));
    await settle(fixture);

    const radios = [...el.querySelectorAll<HTMLInputElement>('input[type="radio"]')];
    expect(radios.map((r) => r.disabled)).toEqual([true, false, true]);
    const badges = [...el.querySelectorAll('fieldset [hlmBadge]')].map((b) =>
      b.textContent?.trim(),
    );
    expect(badges).toEqual(['Connected here', 'In another Project']);

    radios[1].click();
    await settle(fixture);
    el.querySelector('form')!.dispatchEvent(new Event('submit'));
    await settle(fixture);
    const create = http.expectOne(`${v1}/projects/prj_1/sources`);
    expect(create.request.body).toEqual({ connection_id: 'vcn_1', external_repository_id: '8' });
    create.flush(
      ok({ ...api, id: 'src_2', external_repository_id: '8', full_name: 'northwind/web' }),
    );
    await settle(fixture);

    http.expectOne(sourcesURL).flush(page([api]));
    await settle(fixture);
    expect(el.querySelector('form')).toBeNull();
  });

  it('offers no changes to someone who cannot manage Sources here', async () => {
    const { el } = await render([]);
    expect(el.querySelector('tbody')?.textContent).toContain('northwind/api');
    expect(el.querySelectorAll('button').length).toBe(0);
  });

  it('points to Connections when the Organisation has none to pick from', async () => {
    const { fixture, el } = await render();
    await openConnect(fixture, el, [{ ...connection, deleted_at: '2026-11-08T11:00:00Z' }]);
    expect(el.textContent).toContain('no active Connection');
    expect(el.querySelector('a[href="/settings/connections"]')).not.toBeNull();
  });
});
