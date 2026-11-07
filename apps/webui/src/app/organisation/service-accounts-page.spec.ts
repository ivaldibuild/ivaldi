import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { signal } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideRouter, Router } from '@angular/router';
import { CurrentUser } from '../api/current-user';
import { API_BASE_URL } from '../api-base-url';
import { ServiceAccountsPage } from './service-accounts-page';

const v1 = 'https://cp.test/v1';
const ok = <T>(data: T) => ({ status: 'success', data });

const github = {
  id: 'prn_gha',
  display_name: 'GitHub Actions',
  description: 'Creates Releases from tags',
  status: 'active',
  created_by_principal_id: 'prn_ama',
  created_at: '2026-11-01T10:00:00Z',
  updated_at: '2026-11-01T10:00:00Z',
};
const old = {
  ...github,
  id: 'prn_old',
  display_name: 'Jenkins',
  description: '',
  status: 'suspended',
};

describe('ServiceAccountsPage', () => {
  let http: HttpTestingController;

  async function render(
    role: string,
  ): Promise<{ fixture: ComponentFixture<ServiceAccountsPage>; el: HTMLElement }> {
    TestBed.configureTestingModule({
      imports: [ServiceAccountsPage],
      providers: [
        provideHttpClient(),
        provideHttpClientTesting(),
        provideRouter([]),
        { provide: API_BASE_URL, useValue: 'https://cp.test' },
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
    const fixture = TestBed.createComponent(ServiceAccountsPage);
    fixture.detectChanges();
    await settle(fixture);
    http.expectOne(`${v1}/service-accounts`).flush(ok([github]));
    await settle(fixture);
    return { fixture, el: fixture.nativeElement as HTMLElement };
  }

  async function settle(fixture: ComponentFixture<unknown>): Promise<void> {
    for (let i = 0; i < 3; i++) {
      await new Promise((resolve) => setTimeout(resolve));
      fixture.detectChanges();
    }
  }

  function button(el: HTMLElement, text: string): HTMLButtonElement | undefined {
    return [...el.querySelectorAll('button')].find((b) => b.textContent?.trim().includes(text));
  }

  afterEach(() => http.verify());

  it('lists service accounts, and suspended ones on request', async () => {
    const { fixture, el } = await render('member');

    const link = el.querySelector<HTMLAnchorElement>('tbody a')!;
    expect(link.textContent?.trim()).toBe('GitHub Actions');
    expect(link.getAttribute('href')).toBe('/settings/service-accounts/prn_gha');
    expect(el.textContent).toContain('Creates Releases from tags');

    const toggle = el.querySelector<HTMLInputElement>('input[type="checkbox"]')!;
    toggle.checked = true;
    toggle.dispatchEvent(new Event('change'));
    await settle(fixture);
    http.expectOne(`${v1}/service-accounts?include_inactive=true`).flush(ok([github, old]));
    await settle(fixture);
    expect(el.querySelectorAll('tbody tr').length).toBe(2);
    expect(el.textContent).toContain('suspended');
  });

  it('is read-only for members', async () => {
    const { el } = await render('member');

    expect(button(el, 'New service account')).toBeUndefined();
  });

  it('creates a service account and opens it', async () => {
    const { fixture, el } = await render('admin');
    const navigate = vi.spyOn(TestBed.inject(Router), 'navigate').mockResolvedValue(true);

    button(el, 'New service account')!.click();
    await settle(fixture);
    const name = el.querySelector<HTMLInputElement>('#sa-name')!;
    name.value = '  GitLab CI ';
    name.dispatchEvent(new Event('input'));
    el.querySelector('form')!.dispatchEvent(new Event('submit'));
    await settle(fixture);

    const create = http.expectOne((r) => r.method === 'POST' && r.url === `${v1}/service-accounts`);
    expect(create.request.body).toEqual({ display_name: 'GitLab CI' });
    create.flush(ok({ ...github, id: 'prn_glc', display_name: 'GitLab CI' }));
    await settle(fixture);

    expect(navigate).toHaveBeenCalledWith(['/settings/service-accounts', 'prn_glc']);
  });

  it('asks for a name before creating one', async () => {
    const { fixture, el } = await render('owner');

    button(el, 'New service account')!.click();
    await settle(fixture);
    el.querySelector('form')!.dispatchEvent(new Event('submit'));
    await settle(fixture);

    expect(el.textContent).toContain('Name it, such as GitHub Actions.');
  });
});
