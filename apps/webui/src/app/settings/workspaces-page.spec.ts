import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { signal } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { CurrentUser } from '../api/current-user';
import { API_BASE_URL } from '../api-base-url';
import { WorkspaceContext } from '../delivery/workspace-context';
import { WorkspacesPage } from './workspaces-page';

const v1 = 'https://cp.test/v1';
const ok = <T>(data: T) => ({ status: 'success', data });

const platform = {
  id: 'wsp_1',
  organization_id: 'org_1',
  slug: 'platform',
  display_name: 'Platform',
  description: 'Shared services',
  is_default: true,
  archived_at: null,
  created_by_principal_id: 'prn_ama',
  created_at: '2026-11-01T10:00:00Z',
  updated_at: '2026-11-01T10:00:00Z',
};
const payments = {
  ...platform,
  id: 'wsp_2',
  slug: 'payments',
  display_name: 'Payments',
  description: '',
  is_default: false,
};
const legacy = {
  ...payments,
  id: 'wsp_3',
  slug: 'legacy',
  display_name: 'Legacy',
  archived_at: '2026-11-03T10:00:00Z',
};

describe('WorkspacesPage', () => {
  let http: HttpTestingController;
  let reload: ReturnType<typeof vi.fn>;

  async function render(
    role: string,
  ): Promise<{ fixture: ComponentFixture<WorkspacesPage>; el: HTMLElement }> {
    reload = vi.fn(() => Promise.resolve());
    TestBed.configureTestingModule({
      imports: [WorkspacesPage],
      providers: [
        provideHttpClient(),
        provideHttpClientTesting(),
        { provide: API_BASE_URL, useValue: 'https://cp.test' },
        {
          provide: CurrentUser,
          useValue: {
            organization: signal({ id: 'org_1', slug: 'northwind', display_name: 'Northwind' }),
            canManageOrganization: signal(['owner', 'admin'].includes(role)),
          },
        },
        { provide: WorkspaceContext, useValue: { reload } },
      ],
    });
    http = TestBed.inject(HttpTestingController);
    const fixture = TestBed.createComponent(WorkspacesPage);
    fixture.detectChanges();
    await settle(fixture);
    http.expectOne(`${v1}/workspaces`).flush(ok([platform, payments]));
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
    return [...el.querySelectorAll('button')].find(
      (b) => b.getAttribute('aria-label') === label || b.textContent?.trim() === label,
    );
  }

  function type(el: HTMLElement, selector: string, value: string): void {
    const input = el.querySelector<HTMLInputElement>(selector)!;
    input.value = value;
    input.dispatchEvent(new Event('input'));
  }

  function rows(el: HTMLElement): string[] {
    return [...el.querySelectorAll('tbody tr')].map(
      (tr) => tr.textContent?.replace(/\s+/g, ' ').trim() ?? '',
    );
  }

  /** The list and the sidebar's switcher both reload after a change. */
  async function expectRefresh(
    fixture: ComponentFixture<unknown>,
    workspaces: unknown[],
  ): Promise<void> {
    http.expectOne(`${v1}/workspaces`).flush(ok(workspaces));
    await settle(fixture);
    expect(reload).toHaveBeenCalled();
  }

  afterEach(() => http.verify());

  it('lists the Workspaces, marking the default, and archived ones on request', async () => {
    const { fixture, el } = await render('member');

    expect(rows(el)[0]).toContain('Platform Default');
    expect(rows(el)[0]).toContain('platform · Shared services');
    expect(rows(el)[1]).toContain('Payments');
    expect(rows(el)[1]).not.toContain('Default');

    const toggle = el.querySelector<HTMLInputElement>('input[type="checkbox"]')!;
    toggle.checked = true;
    toggle.dispatchEvent(new Event('change'));
    await settle(fixture);
    http
      .expectOne(`${v1}/workspaces?include_archived=true`)
      .flush(ok([platform, payments, legacy]));
    await settle(fixture);
    expect(rows(el)[2]).toContain('Legacy Archived');
  });

  it('is read-only for members', async () => {
    const { el } = await render('member');

    expect(button(el, 'New Workspace')).toBeUndefined();
    expect(el.querySelector('tbody button')).toBeNull();
  });

  it('creates a Workspace, its address following its name', async () => {
    const { fixture, el } = await render('admin');

    button(el, 'New Workspace')!.click();
    await settle(fixture);
    type(el, '#workspace-name', 'Data Platform');
    await settle(fixture);
    expect(el.querySelector<HTMLInputElement>('#workspace-slug')!.value).toBe('data-platform');
    el.querySelector('form')!.dispatchEvent(new Event('submit'));
    await settle(fixture);

    const create = http.expectOne((r) => r.method === 'POST' && r.url === `${v1}/workspaces`);
    expect(create.request.body).toEqual({ display_name: 'Data Platform', slug: 'data-platform' });
    create.flush(
      ok({ ...payments, id: 'wsp_4', slug: 'data-platform', display_name: 'Data Platform' }),
    );
    await settle(fixture);
    await expectRefresh(fixture, [platform, payments]);
    expect(el.querySelector('#workspace-name')).toBeNull();
  });

  it('checks the address before creating one', async () => {
    const { fixture, el } = await render('owner');

    button(el, 'New Workspace')!.click();
    await settle(fixture);
    type(el, '#workspace-name', 'Payments');
    type(el, '#workspace-slug', 'Payments Team');
    el.querySelector('form')!.dispatchEvent(new Event('submit'));
    await settle(fixture);

    expect(el.textContent).toContain(
      'Use lower-case letters and digits, with single hyphens between words.',
    );
  });

  it('sends only what changed when a Workspace is edited', async () => {
    const { fixture, el } = await render('admin');

    button(el, 'Edit Payments')!.click();
    await settle(fixture);
    expect(el.querySelector<HTMLInputElement>('#edit-workspace-slug')!.value).toBe('payments');
    type(el, '#edit-workspace-name', 'Payments & Billing');
    el.querySelector('form[aria-label="Edit Payments"]')!.dispatchEvent(new Event('submit'));
    await settle(fixture);

    const update = http.expectOne(
      (r) => r.method === 'PATCH' && r.url === `${v1}/workspaces/wsp_2`,
    );
    expect(update.request.body).toEqual({ display_name: 'Payments & Billing' });
    update.flush(ok({ ...payments, display_name: 'Payments & Billing' }));
    await settle(fixture);
    await expectRefresh(fixture, [platform, { ...payments, display_name: 'Payments & Billing' }]);
    expect(rows(el)[1]).toContain('Payments & Billing');
  });

  it('makes a Workspace the default', async () => {
    const { fixture, el } = await render('admin');

    // The default Workspace can be neither made the default again nor archived.
    expect(button(el, 'Make Platform the default')).toBeUndefined();
    expect(button(el, 'Archive Platform')).toBeUndefined();

    button(el, 'Make Payments the default')!.click();
    await settle(fixture);
    http
      .expectOne((r) => r.method === 'POST' && r.url === `${v1}/workspaces/wsp_2/default`)
      .flush(ok({ ...payments, is_default: true }));
    await settle(fixture);
    await expectRefresh(fixture, [
      { ...platform, is_default: false },
      { ...payments, is_default: true },
    ]);
    expect(rows(el)[1]).toContain('Payments Default');
  });

  it('shows why the control plane refuses to archive a Workspace', async () => {
    const { fixture, el } = await render('owner');

    button(el, 'Archive Payments')!.click();
    await settle(fixture);
    http
      .expectOne((r) => r.method === 'POST' && r.url === `${v1}/workspaces/wsp_2/archive`)
      .flush(
        { title: 'Conflict', status: 409, detail: 'the workspace still has active projects' },
        { status: 409, statusText: 'Conflict' },
      );
    await settle(fixture);

    expect(el.querySelector('[role="alert"]')?.textContent).toContain(
      'The workspace still has active projects.',
    );
    expect(reload).not.toHaveBeenCalled();
  });

  it('restores an archived Workspace', async () => {
    const { fixture, el } = await render('admin');

    const toggle = el.querySelector<HTMLInputElement>('input[type="checkbox"]')!;
    toggle.checked = true;
    toggle.dispatchEvent(new Event('change'));
    await settle(fixture);
    http
      .expectOne(`${v1}/workspaces?include_archived=true`)
      .flush(ok([platform, payments, legacy]));
    await settle(fixture);

    button(el, 'Restore Legacy')!.click();
    await settle(fixture);
    http
      .expectOne((r) => r.method === 'POST' && r.url === `${v1}/workspaces/wsp_3/restore`)
      .flush(ok({ ...legacy, archived_at: null }));
    await settle(fixture);
    http
      .expectOne(`${v1}/workspaces?include_archived=true`)
      .flush(ok([platform, payments, { ...legacy, archived_at: null }]));
    await settle(fixture);
    expect(reload).toHaveBeenCalled();
    expect(rows(el)[2]).not.toContain('Archived');
  });
});
