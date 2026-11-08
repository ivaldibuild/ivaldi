import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { signal } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { ActivatedRoute, convertToParamMap, Params } from '@angular/router';
import { CurrentUser } from '../api/current-user';
import { API_BASE_URL } from '../api-base-url';
import { BrowserLocation } from '../auth/browser-location';
import { ConnectionsPage } from './connections-page';

const v1 = 'https://cp.test/v1';
const ok = <T>(data: T) => ({ status: 'success', data });

const northwind = {
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

describe('ConnectionsPage', () => {
  let http: HttpTestingController;
  let assign: ReturnType<typeof vi.fn>;

  async function render(
    role: string,
    params: Params = {},
  ): Promise<{ fixture: ComponentFixture<ConnectionsPage>; el: HTMLElement }> {
    assign = vi.fn();
    TestBed.configureTestingModule({
      imports: [ConnectionsPage],
      providers: [
        provideHttpClient(),
        provideHttpClientTesting(),
        { provide: API_BASE_URL, useValue: 'https://cp.test' },
        { provide: BrowserLocation, useValue: { host: 'app.test', assign } },
        {
          provide: ActivatedRoute,
          useValue: { snapshot: { queryParamMap: convertToParamMap(params) } },
        },
        {
          provide: CurrentUser,
          useValue: {
            organization: signal({ id: 'org_1', slug: 'northwind', display_name: 'Northwind' }),
            canManageOrganization: signal(['owner', 'admin'].includes(role)),
          },
        },
      ],
    });
    http = TestBed.inject(HttpTestingController);
    const fixture = TestBed.createComponent(ConnectionsPage);
    fixture.detectChanges();
    await settle(fixture);
    http.expectOne(`${v1}/connections`).flush(ok([northwind]));
    await settle(fixture);
    return { fixture, el: fixture.nativeElement as HTMLElement };
  }

  async function settle(fixture: ComponentFixture<unknown>): Promise<void> {
    for (let i = 0; i < 3; i++) {
      await new Promise((resolve) => setTimeout(resolve));
      fixture.detectChanges();
    }
  }

  function button(el: HTMLElement, label: string): HTMLButtonElement | undefined {
    return [...el.querySelectorAll('button')].find((b) => b.textContent?.includes(label));
  }

  afterEach(() => http.verify());

  it('lists the Connections', async () => {
    const { el } = await render('member');
    expect(el.textContent).toContain('northwind');
    expect(el.textContent).toContain('GitHub · https://github.com');
    expect(button(el, 'Connect GitHub')).toBeUndefined();
  });

  it('sends an admin to GitHub to install the App', async () => {
    const { fixture, el } = await render('admin');
    button(el, 'Connect GitHub')!.click();
    await settle(fixture);
    const request = http.expectOne(`${v1}/connections/installations`);
    expect(request.request.body).toEqual({ provider: 'github' });
    request.flush(
      ok({ url: 'https://github.com/apps/ivaldi/installations/new?state=S', expires_at: '' }),
    );
    await settle(fixture);
    expect(assign).toHaveBeenCalledWith('https://github.com/apps/ivaldi/installations/new?state=S');
  });

  it('says when GitHub is not configured', async () => {
    const { fixture, el } = await render('owner');
    button(el, 'Connect GitHub')!.click();
    await settle(fixture);
    http
      .expectOne(`${v1}/connections/installations`)
      .flush(
        { detail: 'no provider is configured for this connection' },
        { status: 422, statusText: 'Unprocessable Entity' },
      );
    await settle(fixture);
    expect(el.textContent).toContain("GitHub isn't set up for this Ivaldi installation yet");
    expect(assign).not.toHaveBeenCalled();
  });

  it('confirms the account an install just connected', async () => {
    const { el } = await render('owner', { connected: 'northwind' });
    expect(el.querySelector('[role="status"]')?.textContent).toContain('Ivaldi can now reach');
  });
});
