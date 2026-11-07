import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { signal } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { CurrentUser } from '../api/current-user';
import { API_BASE_URL } from '../api-base-url';
import { TeamPage } from './team-page';

const v1 = 'https://cp.test/v1';
const ok = <T>(data: T) => ({ status: 'success', data });
const page = <T>(items: T[]) =>
  ok({ items, total_count: items.length, has_next: false, next_cursor: null });

const member = (id: string, role = 'member', kind = 'user') => ({
  principal_id: id,
  principal_kind: kind,
  role,
  created_at: '2026-11-01T10:00:00Z',
  updated_at: '2026-11-01T10:00:00Z',
});
const payments = {
  id: 'tem_pay',
  organization_id: 'org_1',
  slug: 'payments',
  display_name: 'Payments',
  description: 'Checkout and billing',
  members: [member('prn_ama', 'lead'), member('prn_jonas')],
  created_at: '2026-11-01T10:00:00Z',
  updated_at: '2026-11-01T10:00:00Z',
  archived_at: null as string | null,
};
const person = (id: string, name: string, status = 'active') => ({
  id,
  display_name: name,
  email: `${name.split(' ')[0].toLowerCase()}@northwind.dev`,
  status,
  roles: [],
  last_signed_in_at: null,
});
const people = [
  person('prn_ama', 'Ama Mensah'),
  person('prn_jonas', 'Jonas Berg'),
  person('prn_kofi', 'Kofi Boateng'),
  person('prn_gone', 'Lea Gone', 'suspended'),
];

describe('TeamPage', () => {
  let http: HttpTestingController;

  async function render(
    role: string,
    team = payments,
  ): Promise<{ fixture: ComponentFixture<TeamPage>; el: HTMLElement }> {
    TestBed.configureTestingModule({
      imports: [TeamPage],
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
    const fixture = TestBed.createComponent(TeamPage);
    fixture.componentRef.setInput('id', 'tem_pay');
    fixture.detectChanges();
    await settle(fixture);
    http.expectOne(`${v1}/teams/tem_pay`).flush(ok(team));
    http.expectOne(`${v1}/people?limit=100&include_inactive=true`).flush(page(people));
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
    return [...el.querySelectorAll('button')].find((b) => b.textContent?.trim() === text);
  }

  function text(el: Element | null): string {
    return el?.textContent?.replace(/\s+/g, ' ').trim() ?? '';
  }

  function select(el: HTMLElement, selector: string, value: string): void {
    const s = el.querySelector<HTMLSelectElement>(selector)!;
    s.value = value;
    s.dispatchEvent(new Event('input'));
    s.dispatchEvent(new Event('change'));
  }

  afterEach(() => http.verify());

  it('shows its members by name', async () => {
    const { el } = await render('member');

    expect(text(el.querySelector('h1'))).toBe('Payments');
    const rows = [...el.querySelectorAll('tbody tr')];
    expect(rows.map((r) => text(r.querySelector('th span')))).toEqual(['Ama Mensah', 'Jonas Berg']);
    expect(text(rows[0].querySelector('th div'))).toBe('ama@northwind.dev');
    expect(rows.map((r) => text(r.querySelector('td')))).toEqual(['Lead', 'Member']);
  });

  it('is read-only for members', async () => {
    const { el } = await render('member');

    expect(button(el, 'Archive')).toBeUndefined();
    expect(button(el, 'Add person')).toBeUndefined();
    expect(el.querySelector('tbody select')).toBeNull();
    expect(el.querySelector('#team-name')).toBeNull();
  });

  it('adds an active person who is not a member yet', async () => {
    const { fixture, el } = await render('admin');

    const options = [...el.querySelectorAll<HTMLOptionElement>('#person-to-add option')];
    expect(options.map((o) => o.value)).toEqual(['', 'prn_kofi']);

    select(el, '#person-to-add', 'prn_kofi');
    select(el, '#role-to-add', 'lead');
    await settle(fixture);
    button(el, 'Add person')!.click();
    await settle(fixture);
    const put = http.expectOne(`${v1}/teams/tem_pay/members/prn_kofi`);
    expect(put.request.method).toBe('PUT');
    expect(put.request.body).toEqual({ role: 'lead' });
    put.flush(ok({ ...payments, members: [...payments.members, member('prn_kofi', 'lead')] }));
    await settle(fixture);

    expect(el.querySelectorAll('tbody tr').length).toBe(3);
    expect(text(el.querySelectorAll('tbody tr')[2])).toContain('Kofi Boateng');
  });

  it('changes a role and removes a member', async () => {
    const { fixture, el } = await render('owner');

    select(el, 'select[aria-label="Role of Jonas Berg"]', 'lead');
    await settle(fixture);
    const put = http.expectOne(`${v1}/teams/tem_pay/members/prn_jonas`);
    expect(put.request.body).toEqual({ role: 'lead' });
    put.flush(
      ok({ ...payments, members: [member('prn_ama', 'lead'), member('prn_jonas', 'lead')] }),
    );
    await settle(fixture);

    el.querySelector<HTMLButtonElement>('button[aria-label="Remove Jonas Berg"]')!.click();
    await settle(fixture);
    const remove = http.expectOne(`${v1}/teams/tem_pay/members/prn_jonas`);
    expect(remove.request.method).toBe('DELETE');
    remove.flush(ok({ ...payments, members: [member('prn_ama', 'lead')] }));
    await settle(fixture);

    expect(el.querySelectorAll('tbody tr').length).toBe(1);
  });

  it('adds an Agent and shows it by name', async () => {
    const { fixture, el } = await render('admin');

    button(el, 'Add an Agent')!.click();
    await settle(fixture);
    const name = el.querySelector<HTMLInputElement>('#agent-name')!;
    name.value = 'Release helper';
    name.dispatchEvent(new Event('input'));
    el.querySelector('form[aria-labelledby="new-agent-heading"]')!.dispatchEvent(
      new Event('submit'),
    );
    await settle(fixture);

    const create = http.expectOne(`${v1}/teams/tem_pay/agents`);
    expect(create.request.body).toEqual({ display_name: 'Release helper', role: 'member' });
    create.flush(
      ok({
        agent: { id: 'prn_bot', display_name: 'Release helper', kind: 'agent' },
        team: { ...payments, members: [...payments.members, member('prn_bot', 'member', 'agent')] },
      }),
    );
    await settle(fixture);

    const agent = el.querySelectorAll('tbody tr')[2];
    expect(text(agent.querySelector('th span'))).toBe('Release helper');
    expect(text(agent.querySelector('th div'))).toBe('Agent');
    expect(agent.querySelector<HTMLSelectElement>('select')!.value).toBe('member');
    expect(el.querySelector('#agent-name')).toBeNull();
  });

  it('saves its details', async () => {
    const { fixture, el } = await render('admin');

    expect(button(el, 'Save changes')!.disabled).toBe(true);
    const description = el.querySelector<HTMLInputElement>('#team-description')!;
    description.value = '';
    description.dispatchEvent(new Event('input'));
    await settle(fixture);
    button(el, 'Save changes')!.click();
    await settle(fixture);

    const patch = http.expectOne(`${v1}/teams/tem_pay`);
    expect(patch.request.method).toBe('PATCH');
    expect(patch.request.body).toEqual({ display_name: 'Payments', description: '' });
    patch.flush(ok({ ...payments, description: null }));
    await settle(fixture);

    expect(button(el, 'Save changes')!.disabled).toBe(true);
  });

  it('shows why it cannot be archived', async () => {
    const { fixture, el } = await render('admin');

    button(el, 'Archive')!.click();
    await settle(fixture);
    http
      .expectOne(`${v1}/teams/tem_pay/archive`)
      .flush(
        { title: 'Conflict', status: 409, detail: 'the team still owns active projects' },
        { status: 409, statusText: 'Conflict' },
      );
    await settle(fixture);

    expect(el.querySelector('[role="alert"]')?.textContent).toContain(
      'The team still owns active projects',
    );
  });

  it('restores an archived Team', async () => {
    const { fixture, el } = await render('admin', {
      ...payments,
      members: [],
      archived_at: '2026-11-02T10:00:00Z',
    });

    expect(el.textContent).toContain('An archived Team has no members');
    expect(button(el, 'Add person')).toBeUndefined();
    button(el, 'Restore')!.click();
    await settle(fixture);
    http.expectOne(`${v1}/teams/tem_pay/restore`).flush(ok(payments));
    await settle(fixture);

    expect(button(el, 'Archive')).toBeDefined();
    expect(button(el, 'Add person')).toBeDefined();
  });
});
