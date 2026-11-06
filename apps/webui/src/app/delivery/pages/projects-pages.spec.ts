import { provideHttpClient } from '@angular/common/http';
import {
  HttpTestingController,
  provideHttpClientTesting,
  TestRequest,
} from '@angular/common/http/testing';
import { signal, Type } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideRouter, Router } from '@angular/router';
import { API_BASE_URL } from '../../api-base-url';
import { WorkspaceContext } from '../workspace-context';
import { NewProjectPage } from './new-project-page';
import { ProjectPage } from './project-page';
import { ProjectsPage } from './projects-page';

const v1 = 'https://cp.test/v1';
const ok = <T>(data: T) => ({ status: 'success', data });
const page = <T>(items: T[], next_cursor: string | null = null) =>
  ok({
    items,
    total_count: items.length + (next_cursor ? 1 : 0),
    has_next: !!next_cursor,
    next_cursor,
  });

const workspace = { id: 'wsp_1', slug: 'platform', display_name: 'Platform', is_default: true };
const teams = [
  { id: 'team_1', slug: 'payments', display_name: 'Payments', members: [] },
  { id: 'team_2', slug: 'core', display_name: 'Core', members: [] },
];
const payments = {
  id: 'prj_1',
  workspace_id: 'wsp_1',
  owner_team_id: 'team_1',
  slug: 'payments',
  display_name: 'Payments',
  description: 'Taking money',
  created_at: '2026-11-01T00:00:00Z',
  archived_at: null,
};
const legacy = {
  ...payments,
  id: 'prj_2',
  owner_team_id: 'team_2',
  slug: 'legacy',
  display_name: 'Legacy',
  description: '',
  archived_at: '2026-11-02T00:00:00Z',
};
const lifecycles = [
  { id: 'lc_std', slug: 'standard', display_name: 'Standard', phases: [] },
  { id: 'lc_fix', slug: 'hotfix', display_name: 'Hotfix', phases: [] },
];
const application = {
  id: 'app_1',
  workspace_id: 'wsp_1',
  project_id: 'prj_1',
  owner_team_id: 'team_2',
  slug: 'payments-api',
  display_name: 'payments-api',
  source_id: null,
};
const scope = 'workspace_id=wsp_1&project_id=prj_1';

describe('project pages', () => {
  let http: HttpTestingController;
  let navigate: ReturnType<typeof vi.spyOn>;

  function render<T>(component: Type<T>, inputs: Record<string, unknown> = {}) {
    TestBed.configureTestingModule({
      imports: [component],
      providers: [
        provideHttpClient(),
        provideHttpClientTesting(),
        provideRouter([]),
        { provide: API_BASE_URL, useValue: 'https://cp.test' },
        {
          provide: WorkspaceContext,
          useValue: { current: signal(workspace), workspaces: signal([workspace]) },
        },
      ],
    });
    http = TestBed.inject(HttpTestingController);
    navigate = vi.spyOn(TestBed.inject(Router), 'navigate').mockResolvedValue(true);
    const fixture = TestBed.createComponent(component);
    for (const [name, value] of Object.entries(inputs)) {
      fixture.componentRef.setInput(name, value);
    }
    fixture.detectChanges();
    return { fixture, el: fixture.nativeElement as HTMLElement };
  }

  async function settle(fixture: ComponentFixture<unknown>): Promise<void> {
    for (let i = 0; i < 4; i++) {
      await new Promise((resolve) => setTimeout(resolve));
      fixture.detectChanges();
    }
  }

  /** The one outstanding request to `path`, which may carry a query. */
  function expect1(path: string, method = 'GET'): TestRequest {
    return http.expectOne((r) => r.method === method && r.url === `${v1}${path}`);
  }

  function type(el: HTMLElement, selector: string, value: string): void {
    const input = el.querySelector<HTMLInputElement | HTMLTextAreaElement>(selector)!;
    input.value = value;
    input.dispatchEvent(new Event('input'));
  }

  function choose(el: HTMLElement, selector: string, value: string): void {
    const select = el.querySelector<HTMLSelectElement>(selector)!;
    select.value = value;
    // A browser fires both: Signal Forms listen for input, plain handlers for change.
    select.dispatchEvent(new Event('input'));
    select.dispatchEvent(new Event('change'));
  }

  function click(el: HTMLElement, text: string): void {
    const button = [...el.querySelectorAll('button')].find((b) => b.textContent?.includes(text));
    expect(button, `a button labelled "${text}"`).toBeTruthy();
    button!.click();
  }

  afterEach(() => http.verify());

  describe('the Projects page', () => {
    const firstPage = `/workspaces/wsp_1/projects?sort_by=display_name&limit=50`;

    async function renderList() {
      const rendered = render(ProjectsPage);
      await settle(rendered.fixture);
      expect1('/teams').flush(page(teams));
      return rendered;
    }

    it('lists the Projects with their owning Teams', async () => {
      const { fixture, el } = await renderList();
      expect1(firstPage).flush(page([payments]));
      await settle(fixture);

      const rows = [...el.querySelectorAll('tbody tr')].map((tr) =>
        tr.textContent?.replace(/\s+/g, ' ').trim(),
      );
      expect(rows).toEqual(['Payments payments Payments Taking money']);
      expect(el.querySelector('tbody a')?.getAttribute('href')).toBe('/projects/prj_1');
      expect(el.querySelector('[role="status"]')?.textContent).toContain('Showing 1 of 1');
    });

    it('searches, filters by Team, and shows archived Projects on request', async () => {
      const { fixture, el } = await renderList();
      expect1(firstPage).flush(page([payments]));
      await settle(fixture);

      type(el, '#project-search', ' leg ');
      el.querySelector('form')!.dispatchEvent(new Event('submit'));
      await settle(fixture);
      expect1(`${firstPage}&q=leg`).flush(page([]));
      await settle(fixture);
      expect(el.textContent).toContain('No Projects match');

      choose(el, '#project-team', 'team_2');
      await settle(fixture);
      expect1(`${firstPage}&q=leg&owner_team_id=team_2`).flush(page([]));
      await settle(fixture);

      const archived = el.querySelector<HTMLInputElement>('input[type="checkbox"]')!;
      archived.checked = true;
      archived.dispatchEvent(new Event('change'));
      await settle(fixture);
      expect1(`${firstPage}&q=leg&owner_team_id=team_2&include_archived=true`).flush(
        page([legacy]),
      );
      await settle(fixture);
      expect(el.querySelector('tbody')?.textContent).toContain('Archived');

      click(el, 'Clear filters');
      await settle(fixture);
      expect1(firstPage).flush(page([payments]));
      await settle(fixture);
      expect(el.querySelector<HTMLInputElement>('#project-search')!.value).toBe('');
    });

    it('shows more Projects from the next page', async () => {
      const { fixture, el } = await renderList();
      expect1(firstPage).flush(page([payments], 'cursor-2'));
      await settle(fixture);

      click(el, 'Show more');
      await settle(fixture);
      expect1(`${firstPage}&next_cursor=cursor-2`).flush(page([legacy]));
      await settle(fixture);

      expect(el.querySelectorAll('tbody tr')).toHaveLength(2);
      expect(
        [...el.querySelectorAll('button')].some((b) => b.textContent?.includes('Show more')),
      ).toBe(false);
    });

    it('says when the Projects did not load', async () => {
      const { fixture, el } = await renderList();
      expect1(firstPage).flush({ detail: 'nope' }, { status: 500, statusText: 'Server Error' });
      await settle(fixture);
      expect(el.querySelector('[role="alert"]')?.textContent).toContain("Projects didn't load");
    });

    it('offers to create the first Project when there are none', async () => {
      const { fixture, el } = await renderList();
      expect1(firstPage).flush(page([]));
      await settle(fixture);
      expect(el.textContent).toContain('No Projects yet');
    });
  });

  describe('the New project page', () => {
    it('derives the address from the name and creates the Project', async () => {
      const { fixture, el } = render(NewProjectPage);
      await settle(fixture);
      expect1('/teams').flush(page(teams));
      await settle(fixture);

      type(el, '#project-name', 'Payments Platform');
      await settle(fixture);
      expect(el.querySelector<HTMLInputElement>('#project-slug')!.value).toBe('payments-platform');
      choose(el, '#project-team', 'team_2');
      type(el, '#project-description', ' Money in and out ');
      el.querySelector('#project-form')!.dispatchEvent(new Event('submit'));
      await settle(fixture);

      const create = expect1('/workspaces/wsp_1/projects', 'POST');
      expect(create.request.body).toEqual({
        display_name: 'Payments Platform',
        slug: 'payments-platform',
        owner_team_id: 'team_2',
        description: 'Money in and out',
      });
      create.flush(ok({ ...payments, id: 'prj_new' }));
      await settle(fixture);
      expect(navigate).toHaveBeenCalledWith(['/projects', 'prj_new']);
    });

    it('keeps an address that was typed, and refuses an invalid one', async () => {
      const { fixture, el } = render(NewProjectPage);
      await settle(fixture);
      expect1('/teams').flush(page(teams));
      await settle(fixture);

      type(el, '#project-slug', 'Not A Slug');
      type(el, '#project-name', 'Payments');
      el.querySelector('#project-form')!.dispatchEvent(new Event('submit'));
      await settle(fixture);

      expect(el.querySelector<HTMLInputElement>('#project-slug')!.value).toBe('Not A Slug');
      expect(el.textContent).toContain('Use lower-case letters and digits');
    });
  });

  describe('the Project page', () => {
    async function renderProject(
      project: typeof payments | typeof legacy = payments,
      resolved: object = {
        key: 'delivery.default_lifecycle',
        value: 'lc_std',
        level: 'workspace',
        from_default: false,
        locked: false,
      },
      stored: object[] = [],
    ) {
      const rendered = render(ProjectPage, { id: 'prj_1' });
      await settle(rendered.fixture);
      expect1('/projects/prj_1').flush(ok({ ...project, id: 'prj_1' }));
      await settle(rendered.fixture);
      expect1('/teams').flush(page(teams));
      expect1('/projects/prj_1/applications?limit=100').flush(page([application]));
      expect1('/workspaces/wsp_1/lifecycles').flush(page(lifecycles));
      expect1(`/settings/delivery.default_lifecycle/resolved?${scope}`).flush(ok(resolved));
      expect1(`/settings?${scope}`).flush(ok(stored));
      await settle(rendered.fixture);
      return rendered;
    }

    /** Answers the reload that follows a change. */
    async function reload(fixture: ComponentFixture<unknown>, project = payments) {
      await settle(fixture);
      expect1('/projects/prj_1').flush(ok(project));
      await settle(fixture);
      expect1('/teams').flush(page(teams));
      expect1('/projects/prj_1/applications?limit=100').flush(page([application]));
      expect1('/workspaces/wsp_1/lifecycles').flush(page(lifecycles));
      expect1(`/settings/delivery.default_lifecycle/resolved?${scope}`).flush(
        ok({
          key: 'delivery.default_lifecycle',
          value: 'lc_std',
          level: 'workspace',
          from_default: false,
          locked: false,
        }),
      );
      expect1(`/settings?${scope}`).flush(ok([]));
      await settle(fixture);
    }

    it('shows the Project, its Applications, and where its default Lifecycle comes from', async () => {
      const { el } = await renderProject();
      expect(el.querySelector('h1')?.textContent).toContain('Payments');
      expect(el.textContent).toContain('Owned by Payments');
      expect(el.textContent).toContain('Taking money');
      const app = el.querySelector('section[aria-labelledby="applications-heading"] li');
      expect(app?.textContent?.replace(/\s+/g, ' ')).toContain('payments-api Owned by Core');
      expect(el.textContent?.replace(/\s+/g, ' ')).toContain(
        'follow Standard. Set on the Workspace.',
      );
      expect(
        [...el.querySelectorAll('button')].some((b) =>
          b.textContent?.includes("Use the Workspace's"),
        ),
      ).toBe(false);
    });

    it('sets the default Lifecycle on the Project, and goes back to the Workspace’s', async () => {
      const { fixture, el } = await renderProject();
      choose(el, '#project-lifecycle', 'lc_fix');
      click(el, 'Set for this Project');
      await settle(fixture);
      const put = expect1(`/settings/delivery.default_lifecycle?${scope}`, 'PUT');
      expect(put.request.body).toEqual({ value: 'lc_fix', expected_version: 0 });
      put.flush(ok({ key: 'delivery.default_lifecycle', value: 'lc_fix', version: 1 }));
      await reload(fixture);

      TestBed.resetTestingModule();
      const second = await renderProject(
        payments,
        {
          key: 'delivery.default_lifecycle',
          value: 'lc_fix',
          level: 'project',
          from_default: false,
          locked: false,
        },
        [{ key: 'delivery.default_lifecycle', value: 'lc_fix', version: 3 }],
      );
      expect(second.el.textContent).toContain('Set on this Project.');
      click(second.el, "Use the Workspace's");
      await settle(second.fixture);
      expect1(`/settings/delivery.default_lifecycle?${scope}&expected_version=3`, 'DELETE').flush(
        null,
        { status: 204, statusText: 'No Content' },
      );
      await reload(second.fixture);
    });

    it('says when a lock fixes the default Lifecycle', async () => {
      const { el } = await renderProject(payments, {
        key: 'delivery.default_lifecycle',
        value: 'lc_std',
        level: 'workspace',
        from_default: false,
        locked: true,
      });
      expect(el.textContent).toContain('A policy lock fixes this value');
      expect(el.querySelector('#project-lifecycle')).toBeNull();
    });

    it('saves only the details that changed', async () => {
      const { fixture, el } = await renderProject();
      type(el, '#details-name', 'Payments Core');
      el.querySelector('#details-form')!.dispatchEvent(new Event('submit'));
      await settle(fixture);
      const patch = expect1('/projects/prj_1', 'PATCH');
      expect(patch.request.body).toEqual({ display_name: 'Payments Core' });
      patch.flush(ok({ ...payments, display_name: 'Payments Core' }));
      await reload(fixture, { ...payments, display_name: 'Payments Core' });
      expect(el.querySelector('[role="status"]')?.textContent).toContain('Saved.');

      el.querySelector('#details-form')!.dispatchEvent(new Event('submit'));
      await settle(fixture);
      expect(el.textContent).toContain('Nothing to save.');
    });

    it('hands the Project to another Team', async () => {
      const { fixture, el } = await renderProject();
      choose(el, '#project-owner', 'team_2');
      await settle(fixture);
      click(el, 'Hand over');
      await settle(fixture);
      const transfer = expect1('/projects/prj_1/transfer', 'POST');
      expect(transfer.request.body).toEqual({ owner_team_id: 'team_2' });
      transfer.flush(ok({ ...payments, owner_team_id: 'team_2' }));
      await reload(fixture, { ...payments, owner_team_id: 'team_2' });
    });

    it('archives after confirming, and shows why it could not', async () => {
      const { fixture, el } = await renderProject();
      click(el, 'Archive…');
      await settle(fixture);
      click(el, 'Archive project');
      await settle(fixture);
      expect1('/projects/prj_1/archive', 'POST').flush(
        { detail: 'has active dependents: 1 active Application (delivery)' },
        { status: 409, statusText: 'Conflict' },
      );
      await settle(fixture);
      expect(el.querySelector('[role="alert"]')?.textContent).toContain('1 active Application');
    });

    it('offers to restore an archived Project and nothing else', async () => {
      const { fixture, el } = await renderProject(legacy);
      expect(el.textContent).toContain('This Project is archived');
      expect(el.querySelector('#details-form')).toBeNull();
      expect(el.querySelector('#project-lifecycle')).toBeNull();
      click(el, 'Restore project');
      await settle(fixture);
      expect1('/projects/prj_1/restore', 'POST').flush(ok(payments));
      await reload(fixture);
      expect(el.textContent).not.toContain('This Project is archived');
    });

    it('says when the Project did not load', async () => {
      const { fixture, el } = render(ProjectPage, { id: 'prj_1' });
      await settle(fixture);
      expect1('/projects/prj_1').flush(
        { detail: 'not found' },
        { status: 404, statusText: 'Not Found' },
      );
      await settle(fixture);
      expect(el.querySelector('[role="alert"]')?.textContent).toContain("The Project didn't load");
      expect(el.textContent).not.toContain('Loading the Project');
    });

    it('opens a new Application in this Project', async () => {
      const { el } = await renderProject();
      click(el, 'New application');
      expect(navigate).toHaveBeenCalledWith(['/applications/new'], {
        queryParams: { project: 'prj_1' },
      });
    });
  });
});
