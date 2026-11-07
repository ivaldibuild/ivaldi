import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { signal } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideRouter, Router } from '@angular/router';
import { CurrentUser } from '../api/current-user';
import { API_BASE_URL } from '../api-base-url';
import { AgentsPage } from './agents-page';

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
  teams: [membership],
  has_system_prompt: true,
};
const detail = {
  ...agent,
  system_prompt: 'Ship carefully.',
};
const team = {
  id: 'tem_pay',
  organization_id: 'org_1',
  slug: 'payments',
  display_name: 'Payments',
  description: null,
  members: [],
  created_at: '2026-11-01T10:00:00Z',
  updated_at: '2026-11-01T10:00:00Z',
  archived_at: null,
};

describe('AgentsPage', () => {
  let http: HttpTestingController;

  async function render(
    role: string,
  ): Promise<{ fixture: ComponentFixture<AgentsPage>; el: HTMLElement }> {
    TestBed.configureTestingModule({
      imports: [AgentsPage],
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
    const fixture = TestBed.createComponent(AgentsPage);
    fixture.detectChanges();
    await settle(fixture);
    http.expectOne(`${v1}/agents?limit=100`).flush(page([agent]));
    http.expectOne(`${v1}/teams?limit=100`).flush(page([team]));
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

  it('lists Agents with their Teams, status, and prompt indicator', async () => {
    const { el } = await render('member');

    expect(el.querySelector('tbody a')?.textContent?.trim()).toBe('Release helper');
    expect(el.textContent).toContain('Payments');
    expect(el.textContent).toContain('active');
    expect(el.textContent).toContain('Set');
  });

  it('is read-only for members', async () => {
    const { el } = await render('member');

    expect(button(el, 'New Agent')).toBeUndefined();
  });

  it('creates an Agent with a system prompt and opens it', async () => {
    const { fixture, el } = await render('admin');
    const navigate = vi.spyOn(TestBed.inject(Router), 'navigate').mockResolvedValue(true);

    button(el, 'New Agent')!.click();
    await settle(fixture);
    const name = el.querySelector<HTMLInputElement>('#agent-name')!;
    name.value = 'Triage helper';
    name.dispatchEvent(new Event('input'));
    const prompt = el.querySelector<HTMLTextAreaElement>('#agent-prompt')!;
    prompt.value = 'Look at the evidence first.';
    prompt.dispatchEvent(new Event('input'));
    await settle(fixture);
    el.querySelector('form')!.dispatchEvent(new Event('submit'));
    await settle(fixture);

    const create = http.expectOne(`${v1}/agents`);
    expect(create.request.method).toBe('POST');
    expect(create.request.body).toEqual({
      display_name: 'Triage helper',
      team_id: 'tem_pay',
      role: 'member',
      system_prompt: 'Look at the evidence first.',
    });
    create.flush(ok({ ...detail, id: 'prn_triage', display_name: 'Triage helper' }));
    await settle(fixture);

    expect(navigate).toHaveBeenCalledWith(['/settings/agents', 'prn_triage']);
  });
});
