import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { signal } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { CurrentUser } from '../api/current-user';
import { API_BASE_URL } from '../api-base-url';
import { AgentPage } from './agent-page';

const v1 = 'https://cp.test/v1';
const ok = <T>(data: T) => ({ status: 'success', data });
const page = <T>(items: T[]) =>
  ok({ items, total_count: items.length, has_next: false, next_cursor: null });

const membership = {
  team_id: 'tem_pay',
  slug: 'payments',
  display_name: 'Payments',
  role: 'member',
  archived_at: null as string | null,
};
const agent = {
  id: 'prn_bot',
  organization_id: 'org_1',
  kind: 'agent',
  display_name: 'Release helper',
  status: 'active',
  metadata: {},
  created_at: '2026-11-01T10:00:00Z',
  updated_at: '2026-11-01T10:00:00Z',
  slug: 'release-helper',
  system_prompt: 'Ship carefully.',
  teams: [membership],
};
const team = (id: string, name: string) => ({
  id,
  organization_id: 'org_1',
  slug: name.toLowerCase(),
  display_name: name,
  description: null,
  members: [],
  created_at: '2026-11-01T10:00:00Z',
  updated_at: '2026-11-01T10:00:00Z',
  archived_at: null as string | null,
});

describe('AgentPage', () => {
  let http: HttpTestingController;

  async function render(
    role: string,
    data = agent,
    teams = [team('tem_pay', 'Payments')],
  ): Promise<{ fixture: ComponentFixture<AgentPage>; el: HTMLElement }> {
    TestBed.configureTestingModule({
      imports: [AgentPage],
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
    const fixture = TestBed.createComponent(AgentPage);
    fixture.componentRef.setInput('id', 'prn_bot');
    fixture.detectChanges();
    await settle(fixture);
    http.expectOne(`${v1}/agents/prn_bot`).flush(ok(data));
    http.expectOne(`${v1}/teams?limit=100`).flush(page(teams));
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

  afterEach(() => http.verify());

  it('shows the system prompt and Team memberships', async () => {
    const { el } = await render('member');

    expect(el.textContent).toContain('Ship carefully.');
    expect(el.querySelector('tbody a')?.textContent?.trim()).toBe('Payments');
    expect(el.querySelector('#agent-prompt')).toBeNull();
  });

  it('saves a changed system prompt', async () => {
    const { fixture, el } = await render('admin');

    const prompt = el.querySelector<HTMLTextAreaElement>('#agent-prompt')!;
    prompt.value = 'Prefer the graph.';
    prompt.dispatchEvent(new Event('input'));
    await settle(fixture);
    button(el, 'Save changes')!.click();
    await settle(fixture);

    const patch = http.expectOne(`${v1}/agents/prn_bot`);
    expect(patch.request.method).toBe('PATCH');
    expect(patch.request.body).toEqual({ system_prompt: 'Prefer the graph.' });
    patch.flush(ok({ ...agent, system_prompt: 'Prefer the graph.' }));
    await settle(fixture);

    expect((el.querySelector('#agent-prompt') as HTMLTextAreaElement).value).toBe(
      'Prefer the graph.',
    );
  });

  it('suspends and reactivates the Agent', async () => {
    const { fixture, el } = await render('admin');

    button(el, 'Suspend')!.click();
    await settle(fixture);
    http
      .expectOne(`${v1}/agents/prn_bot/suspend`)
      .flush(ok({ ...agent, status: 'suspended' }));
    await settle(fixture);
    expect(button(el, 'Reactivate')).toBeDefined();

    button(el, 'Reactivate')!.click();
    await settle(fixture);
    http
      .expectOne(`${v1}/agents/prn_bot/reactivate`)
      .flush(ok({ ...agent, status: 'active' }));
    await settle(fixture);
    expect(button(el, 'Suspend')).toBeDefined();
  });

  it('adds the Agent to another Team', async () => {
    const { fixture, el } = await render('admin', agent, [
      team('tem_pay', 'Payments'),
      team('tem_ops', 'Operations'),
    ]);

    const select = el.querySelector<HTMLSelectElement>('#team-to-add')!;
    select.value = 'tem_ops';
    select.dispatchEvent(new Event('change'));
    await settle(fixture);
    el.querySelector('form[aria-label="Add to Team"]')!.dispatchEvent(new Event('submit'));
    await settle(fixture);

    const put = http.expectOne(`${v1}/teams/tem_ops/members/prn_bot`);
    expect(put.request.method).toBe('PUT');
    expect(put.request.body).toEqual({ role: 'member' });
    put.flush(ok(team('tem_ops', 'Operations')));
    await settle(fixture);
    http.expectOne(`${v1}/agents/prn_bot`).flush(
      ok({
        ...agent,
        teams: [
          membership,
          {
            team_id: 'tem_ops',
            slug: 'operations',
            display_name: 'Operations',
            role: 'member',
            archived_at: null,
          },
        ],
      }),
    );
    await settle(fixture);

    expect(el.querySelectorAll('tbody tr').length).toBe(2);
  });
});
