import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { signal } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideRouter, Router } from '@angular/router';
import { CurrentUser } from '../api/current-user';
import { API_BASE_URL } from '../api-base-url';
import { TeamsPage } from './teams-page';

const v1 = 'https://cp.test/v1';
const ok = <T>(data: T) => ({ status: 'success', data });
const page = <T>(items: T[]) =>
  ok({ items, total_count: items.length, has_next: false, next_cursor: null });

const member = (id: string) => ({
  principal_id: id,
  principal_kind: 'user',
  role: 'member',
  created_at: '2026-11-01T10:00:00Z',
  updated_at: '2026-11-01T10:00:00Z',
});
const payments = {
  id: 'tem_pay',
  organization_id: 'org_1',
  slug: 'payments',
  display_name: 'Payments',
  description: 'Checkout and billing',
  members: [member('prn_ama'), member('prn_kofi')],
  created_at: '2026-11-01T10:00:00Z',
  updated_at: '2026-11-01T10:00:00Z',
  archived_at: null,
};
const legacy = {
  ...payments,
  id: 'tem_old',
  slug: 'legacy',
  display_name: 'Legacy',
  description: null,
  members: [],
  archived_at: '2026-11-02T10:00:00Z',
};

describe('TeamsPage', () => {
  let http: HttpTestingController;

  async function render(
    role: string,
  ): Promise<{ fixture: ComponentFixture<TeamsPage>; el: HTMLElement }> {
    TestBed.configureTestingModule({
      imports: [TeamsPage],
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
    const fixture = TestBed.createComponent(TeamsPage);
    fixture.detectChanges();
    await settle(fixture);
    http.expectOne(`${v1}/teams?limit=100`).flush(page([payments]));
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

  it('lists Teams with their members, and archived ones on request', async () => {
    const { fixture, el } = await render('member');

    const link = el.querySelector<HTMLAnchorElement>('tbody a')!;
    expect(link.textContent?.trim()).toBe('Payments');
    expect(link.getAttribute('href')).toBe('/settings/teams/tem_pay');
    expect(el.querySelector('tbody tr')?.textContent).toContain('2 members');
    expect(el.textContent).toContain('Checkout and billing');

    const toggle = el.querySelector<HTMLInputElement>('input[type="checkbox"]')!;
    toggle.checked = true;
    toggle.dispatchEvent(new Event('change'));
    await settle(fixture);
    http.expectOne(`${v1}/teams?limit=100&include_archived=true`).flush(page([payments, legacy]));
    await settle(fixture);
    expect(el.querySelectorAll('tbody tr').length).toBe(2);
    expect(el.querySelectorAll('tbody tr')[1].textContent).toContain('Archived');
  });

  it('is read-only for members', async () => {
    const { el } = await render('member');

    expect(button(el, 'New Team')).toBeUndefined();
  });

  it('creates a Team and opens it', async () => {
    const { fixture, el } = await render('admin');
    const navigate = vi.spyOn(TestBed.inject(Router), 'navigate').mockResolvedValue(true);

    button(el, 'New Team')!.click();
    await settle(fixture);
    const name = el.querySelector<HTMLInputElement>('#team-name')!;
    name.value = ' Platform ';
    name.dispatchEvent(new Event('input'));
    el.querySelector('form')!.dispatchEvent(new Event('submit'));
    await settle(fixture);

    const create = http.expectOne((r) => r.method === 'POST' && r.url === `${v1}/teams`);
    expect(create.request.body).toEqual({ display_name: 'Platform' });
    create.flush(ok({ ...payments, id: 'tem_plat', display_name: 'Platform', members: [] }));
    await settle(fixture);

    expect(navigate).toHaveBeenCalledWith(['/settings/teams', 'tem_plat']);
  });

  it('asks for a name before creating one', async () => {
    const { fixture, el } = await render('owner');

    button(el, 'New Team')!.click();
    await settle(fixture);
    el.querySelector('form')!.dispatchEvent(new Event('submit'));
    await settle(fixture);

    expect(el.textContent).toContain('Name the Team, such as Payments.');
  });
});
