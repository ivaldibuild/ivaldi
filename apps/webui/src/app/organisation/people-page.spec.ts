import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { signal } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { CurrentUser } from '../api/current-user';
import { API_BASE_URL } from '../api-base-url';
import { PeoplePage } from './people-page';

const v1 = 'https://cp.test/v1';
const ok = <T>(data: T) => ({ status: 'success', data });
const page = <T>(items: T[]) =>
  ok({ items, total_count: items.length, has_next: false, next_cursor: null });

const people = [
  {
    id: 'prn_ama',
    display_name: 'Ama Mensah',
    email: 'ama@northwind.dev',
    status: 'active',
    last_signed_in_at: null,
    roles: [{ role: 'owner', role_id: 'r_o' }],
  },
  {
    id: 'prn_jonas',
    display_name: 'Jonas Berg',
    email: 'jonas@northwind.dev',
    status: 'active',
    last_signed_in_at: '2026-11-01T10:00:00Z',
    roles: [{ role: 'member', role_id: 'r_m' }],
  },
  {
    id: 'prn_kofi',
    display_name: 'Kofi Boateng',
    email: 'kofi@northwind.dev',
    status: 'active',
    last_signed_in_at: null,
    roles: [{ role: 'member', role_id: 'r_m' }],
  },
];
const grants = [
  {
    id: 'gr_ama',
    role: 'owner',
    resource_id: 'org_1',
    principal: { id: 'prn_ama', kind: 'user', display_name: 'Ama Mensah', status: 'active' },
    group: null,
    expires_at: null,
  },
  {
    id: 'gr_jonas',
    role: 'member',
    resource_id: 'org_1',
    principal: { id: 'prn_jonas', kind: 'user', display_name: 'Jonas Berg', status: 'active' },
    group: null,
    expires_at: null,
  },
  // Kofi is a member only through his Team's group.
  {
    id: 'gr_team',
    role: 'member',
    resource_id: 'org_1',
    principal: null,
    group: { id: 'grp_1', name: 'Payments', source: 'team', team_id: 'team_1' },
    expires_at: null,
  },
];

describe('PeoplePage', () => {
  let http: HttpTestingController;

  async function render(me: {
    id: string;
    role: string;
  }): Promise<{ fixture: ComponentFixture<PeoplePage>; el: HTMLElement }> {
    TestBed.configureTestingModule({
      imports: [PeoplePage],
      providers: [
        provideHttpClient(),
        provideHttpClientTesting(),
        { provide: API_BASE_URL, useValue: 'https://cp.test' },
        {
          provide: CurrentUser,
          useValue: {
            organization: signal({
              id: 'org_1',
              slug: 'northwind',
              display_name: 'Northwind',
              logo_url: null,
            }),
            profile: signal({ principal: { id: me.id } }),
            role: signal(me.role),
            canManageOrganization: signal(['owner', 'admin'].includes(me.role)),
          },
        },
      ],
    });
    http = TestBed.inject(HttpTestingController);
    const fixture = TestBed.createComponent(PeoplePage);
    fixture.detectChanges();
    await settle(fixture);
    flushList();
    await settle(fixture);
    return { fixture, el: fixture.nativeElement as HTMLElement };
  }

  async function settle(fixture: ComponentFixture<unknown>): Promise<void> {
    for (let i = 0; i < 3; i++) {
      await new Promise((resolve) => setTimeout(resolve));
      fixture.detectChanges();
    }
  }

  function flushList(): void {
    http.expectOne((r) => r.url.startsWith(`${v1}/people`)).flush(page(people));
    http.expectOne((r) => r.url.startsWith(`${v1}/grants`)).flush(page(grants));
  }

  function roleSelect(el: HTMLElement, name: string): HTMLSelectElement | null {
    return el.querySelector<HTMLSelectElement>(`select[aria-label="Role of ${name}"]`);
  }

  afterEach(() => http.verify());

  it('lists people with their Organisation role and where it comes from', async () => {
    const { el } = await render({ id: 'prn_ama', role: 'owner' });

    const rows = [...el.querySelectorAll('tbody tr')].map((tr) =>
      tr.textContent?.replace(/\s+/g, ' ').trim(),
    );
    expect(rows[0]).toContain('Ama Mensah (you)');
    expect(rows[1]).toContain('1 Nov 2026');
    expect(rows[2]).toContain('Through a Team');
    expect(el.textContent).toContain('3 people');
  });

  it('lets an owner promote someone, replacing their old grant', async () => {
    const { fixture, el } = await render({ id: 'prn_ama', role: 'owner' });

    // Owners can hand out every role, but can't change their own.
    expect(roleSelect(el, 'Ama Mensah')).toBeNull();
    const select = roleSelect(el, 'Jonas Berg')!;
    expect([...select.options].map((o) => o.value)).toEqual(['owner', 'admin', 'member', 'viewer']);
    // Each menu starts on the person's current role.
    expect(select.value).toBe('member');
    expect(roleSelect(el, 'Kofi Boateng')!.value).toBe('member');

    select.value = 'admin';
    select.dispatchEvent(new Event('change'));
    await settle(fixture);
    const create = http.expectOne((r) => r.method === 'POST' && r.url === `${v1}/grants`);
    expect(create.request.body).toEqual({
      principal_id: 'prn_jonas',
      role: 'admin',
      resource_id: 'org_1',
    });
    create.flush(ok({ id: 'gr_new', role: 'admin' }));
    await settle(fixture);
    const remove = http.expectOne(`${v1}/grants/gr_jonas`);
    expect(remove.request.method).toBe('DELETE');
    remove.flush(null, { status: 204, statusText: 'No Content' });
    await settle(fixture);

    flushList();
    await settle(fixture);
  });

  it('keeps admins away from the owner role', async () => {
    const { el } = await render({ id: 'prn_jonas', role: 'admin' });

    expect(roleSelect(el, 'Ama Mensah')).toBeNull();
    expect([...roleSelect(el, 'Kofi Boateng')!.options].map((o) => o.value)).toEqual([
      'admin',
      'member',
      'viewer',
    ]);
  });

  it('shows the control plane’s reason when a change is refused', async () => {
    const { fixture, el } = await render({ id: 'prn_ama', role: 'owner' });

    const select = roleSelect(el, 'Jonas Berg')!;
    select.value = 'viewer';
    select.dispatchEvent(new Event('change'));
    await settle(fixture);
    http
      .expectOne((r) => r.method === 'POST' && r.url === `${v1}/grants`)
      .flush({ title: 'Forbidden' }, { status: 403, statusText: 'Forbidden' });
    await settle(fixture);
    flushList();
    await settle(fixture);

    expect(el.querySelector('[role="alert"]')?.textContent).toContain("You don't have permission");
  });

  it('is read-only for members', async () => {
    const { el } = await render({ id: 'prn_jonas', role: 'member' });

    expect(el.querySelector('tbody select')).toBeNull();
  });
});
