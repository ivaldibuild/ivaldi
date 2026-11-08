import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { ActivatedRoute, convertToParamMap, Params, provideRouter, Router } from '@angular/router';
import { API_BASE_URL } from '../api-base-url';
import { BrowserLocation } from '../auth/browser-location';
import { GitHubInstallCallback } from './github-install-callback';

const v1 = 'https://cp.test/v1';
const ok = <T>(data: T) => ({ status: 'success', data });

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

const fromGitHub = { code: 'C', installation_id: '42', state: 'S', setup_action: 'install' };

describe('GitHubInstallCallback', () => {
  let http: HttpTestingController;
  let navigate: ReturnType<typeof vi.spyOn>;
  let assign: ReturnType<typeof vi.fn>;

  async function render(params: Params): Promise<ComponentFixture<GitHubInstallCallback>> {
    assign = vi.fn();
    TestBed.configureTestingModule({
      imports: [GitHubInstallCallback],
      providers: [
        provideHttpClient(),
        provideHttpClientTesting(),
        { provide: API_BASE_URL, useValue: 'https://cp.test' },
        provideRouter([]),
        { provide: BrowserLocation, useValue: { host: 'app.test', assign } },
        {
          provide: ActivatedRoute,
          useValue: { snapshot: { queryParamMap: convertToParamMap(params) } },
        },
      ],
    });
    http = TestBed.inject(HttpTestingController);
    navigate = vi.spyOn(TestBed.inject(Router), 'navigate').mockResolvedValue(true);
    const fixture = TestBed.createComponent(GitHubInstallCallback);
    fixture.detectChanges();
    await settle(fixture);
    return fixture;
  }

  async function settle(fixture: ComponentFixture<unknown>): Promise<void> {
    for (let i = 0; i < 3; i++) {
      await new Promise((resolve) => setTimeout(resolve));
      fixture.detectChanges();
    }
  }

  function text(fixture: ComponentFixture<unknown>): string {
    return (fixture.nativeElement as HTMLElement).textContent ?? '';
  }

  afterEach(() => http.verify());

  it('completes the install and returns to Connections without the code in the URL', async () => {
    const fixture = await render(fromGitHub);
    const request = http.expectOne(`${v1}/connections/installations/complete`);
    expect(request.request.method).toBe('POST');
    expect(request.request.body).toEqual({
      provider: 'github',
      state: 'S',
      code: 'C',
      installation_id: '42',
    });
    request.flush(ok(connection));
    await settle(fixture);

    expect(navigate).toHaveBeenCalledWith(['/settings/connections'], {
      queryParams: { connected: 'northwind' },
      replaceUrl: true,
    });
  });

  it('explains a refusal and can start again', async () => {
    const fixture = await render(fromGitHub);
    http
      .expectOne(`${v1}/connections/installations/complete`)
      .flush(
        { detail: 'the install expired or was started by someone else; start it again' },
        { status: 422, statusText: 'Unprocessable Entity' },
      );
    await settle(fixture);
    expect(text(fixture)).toContain('The install expired or was started by someone else');
    expect(navigate).not.toHaveBeenCalled();

    const startAgain = [...(fixture.nativeElement as HTMLElement).querySelectorAll('button')].find(
      (b) => b.textContent?.includes('Start again'),
    )!;
    startAgain.click();
    await settle(fixture);
    http
      .expectOne(`${v1}/connections/installations`)
      .flush(
        ok({ url: 'https://github.com/apps/ivaldi/installations/new?state=T', expires_at: '' }),
      );
    await settle(fixture);
    expect(assign).toHaveBeenCalledWith('https://github.com/apps/ivaldi/installations/new?state=T');
  });

  it('says when the account is already connected', async () => {
    const fixture = await render(fromGitHub);
    http
      .expectOne(`${v1}/connections/installations/complete`)
      .flush(
        { detail: 'this provider account is already connected' },
        { status: 409, statusText: 'Conflict' },
      );
    await settle(fixture);
    expect(text(fixture)).toContain('already connected, to this Organisation or another');
  });

  it.each([
    [{ ...fromGitHub, state: undefined }, 'was not started from Ivaldi'],
    [{ ...fromGitHub, code: undefined }, 'Request user authorization (OAuth) during installation'],
    [{ error: 'access_denied', state: 'S' }, 'The install was cancelled on GitHub.'],
  ])('does not call the control plane without what it needs (%o)', async (params, message) => {
    const defined = Object.fromEntries(Object.entries(params).filter(([, v]) => v !== undefined));
    const fixture = await render(defined);
    http.expectNone(`${v1}/connections/installations/complete`);
    expect(text(fixture)).toContain(message);
  });
});
