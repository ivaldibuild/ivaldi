import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { signal, Type } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideRouter, Router } from '@angular/router';
import { CurrentUser } from '../../api/current-user';
import { API_BASE_URL } from '../../api-base-url';
import { WorkspaceContext } from '../workspace-context';
import { EnvironmentPage } from './environment-page';
import { EnvironmentsPage } from './environments-page';

const v1 = 'https://cp.test/v1';
const ok = <T>(data: T) => ({ status: 'success', data });
const page = <T>(items: T[]) =>
  ok({ items, total_count: items.length, has_next: false, next_cursor: null });

const workspace = { id: 'wsp_1', slug: 'platform', display_name: 'Platform', is_default: true };
const staging = {
  id: 'env_stg',
  workspace_id: 'wsp_1',
  slug: 'staging',
  display_name: 'Staging',
  tier: 'staging',
  execution_enabled: true,
  deleted_at: null,
};
const target = {
  id: 'tgt_1',
  environment_id: 'env_stg',
  slug: 'staging-eu-1',
  display_name: 'staging-eu-1',
  runtime: 'kubernetes',
  locator: { cluster: 'eu', namespace: 'payments' },
  cluster_id: 'cls_other',
  deleted_at: null,
};

describe('Environment pages', () => {
  let http: HttpTestingController;
  let navigate: ReturnType<typeof vi.spyOn>;

  function render<T>(
    component: Type<T>,
    role: 'admin' | 'member',
    inputs: Record<string, unknown> = {},
  ): { fixture: ComponentFixture<T>; el: HTMLElement } {
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
        {
          provide: CurrentUser,
          useValue: { canManageOrganization: signal(role === 'admin') },
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

  function button(el: HTMLElement, text: string): HTMLButtonElement | undefined {
    return [...el.querySelectorAll('button')].find((b) => b.textContent?.trim() === text);
  }

  function type(el: HTMLElement, selector: string, value: string): void {
    const input = el.querySelector<HTMLInputElement | HTMLSelectElement>(selector)!;
    input.value = value;
    input.dispatchEvent(new Event('input'));
    input.dispatchEvent(new Event('change'));
  }

  afterEach(() => http.verify());

  describe('the Environments page', () => {
    async function open(role: 'admin' | 'member') {
      const rendered = render(EnvironmentsPage, role);
      await settle(rendered.fixture);
      http.expectOne(`${v1}/workspaces/wsp_1/environments`).flush(page([staging]));
      await settle(rendered.fixture);
      http.expectOne(`${v1}/environments/env_stg/targets`).flush(page([target]));
      await settle(rendered.fixture);
      return rendered;
    }

    it('links each Environment to its page', async () => {
      const { el } = await open('member');

      const link = el.querySelector<HTMLAnchorElement>('h2 a')!;
      expect(link.textContent?.trim()).toBe('Staging');
      expect(link.getAttribute('href')).toBe('/environments/env_stg');
      expect(el.textContent).toContain('staging-eu-1');
      expect(button(el, 'New Environment')).toBeUndefined();
    });

    it('shows archived Environments on request', async () => {
      const { fixture, el } = await open('member');

      const toggle = el.querySelector<HTMLInputElement>('input[type="checkbox"]')!;
      toggle.checked = true;
      toggle.dispatchEvent(new Event('change'));
      await settle(fixture);
      http
        .expectOne(`${v1}/workspaces/wsp_1/environments?include_archived=true`)
        .flush(
          page([
            staging,
            { ...staging, id: 'env_old', display_name: 'Old', deleted_at: '2026-11-01T00:00:00Z' },
          ]),
        );
      await settle(fixture);
      http.expectOne(`${v1}/environments/env_stg/targets`).flush(page([target]));
      http.expectOne(`${v1}/environments/env_old/targets`).flush(page([]));
      await settle(fixture);

      expect(el.querySelectorAll('h2 a').length).toBe(2);
      expect(el.textContent).toContain('Archived');
    });

    it('adds an Environment and opens it', async () => {
      const { fixture, el } = await open('admin');

      el.querySelector<HTMLButtonElement>('button[pageActions]')!.click();
      await settle(fixture);
      type(el, '#environment-name', 'Production EU');
      type(el, '#environment-tier', 'production');
      el.querySelector('form')!.dispatchEvent(new Event('submit'));
      await settle(fixture);

      const create = http.expectOne(
        (r) => r.method === 'POST' && r.url === `${v1}/workspaces/wsp_1/environments`,
      );
      expect(create.request.body).toEqual({
        display_name: 'Production EU',
        slug: 'production-eu',
        tier: 'production',
        execution_enabled: true,
      });
      create.flush(ok({ ...staging, id: 'env_prd' }));
      await settle(fixture);

      expect(navigate).toHaveBeenCalledWith(['/environments', 'env_prd']);
    });
  });

  describe('an Environment’s page', () => {
    async function open(role: 'admin' | 'member') {
      const rendered = render(EnvironmentPage, role, { id: 'env_stg' });
      await settle(rendered.fixture);
      flushDetails();
      await settle(rendered.fixture);
      return rendered;
    }

    function flushDetails(environment: object = staging, targets: object[] = [target]): void {
      http.expectOne(`${v1}/environments/env_stg`).flush(ok(environment));
      http
        .expectOne(`${v1}/environments/env_stg/targets?include_archived=true`)
        .flush(page(targets));
    }

    it('saves only what changed', async () => {
      const { fixture, el } = await open('admin');

      expect(el.querySelector('h1')?.textContent?.trim()).toBe('Staging');
      type(el, '#environment-name', 'Staging EU');
      el.querySelector('#environment-name')!.closest('form')!.dispatchEvent(new Event('submit'));
      await settle(fixture);

      const update = http.expectOne(
        (r) => r.method === 'PATCH' && r.url === `${v1}/environments/env_stg`,
      );
      expect(update.request.body).toEqual({ display_name: 'Staging EU' });
      update.flush(ok({ ...staging, display_name: 'Staging EU' }));
      await settle(fixture);
      flushDetails({ ...staging, display_name: 'Staging EU' });
      await settle(fixture);

      expect(el.querySelector('h1')?.textContent?.trim()).toBe('Staging EU');
      expect(el.textContent).toContain('Saved.');
    });

    it('shows why it cannot be archived', async () => {
      const { fixture, el } = await open('admin');

      button(el, 'Archive…')!.click();
      await settle(fixture);
      button(el, 'Archive Environment')!.click();
      await settle(fixture);
      http
        .expectOne((r) => r.method === 'POST' && r.url === `${v1}/environments/env_stg/archive`)
        .flush(
          {
            title: 'Conflict',
            status: 409,
            detail: 'in use: environment env_stg is in 1 active lifecycles',
          },
          { status: 409, statusText: 'Conflict' },
        );
      await settle(fixture);

      const alert = [...el.querySelectorAll('[role="alert"]')].map((a) => a.textContent).join(' ');
      expect(alert).toContain("The Environment wasn't archived");
      expect(alert).toContain('is in 1 active lifecycles');
    });

    it('archives it and offers to restore it', async () => {
      const { fixture, el } = await open('admin');

      button(el, 'Archive…')!.click();
      await settle(fixture);
      button(el, 'Archive Environment')!.click();
      await settle(fixture);
      const archived = { ...staging, deleted_at: '2026-11-07T00:00:00Z' };
      http
        .expectOne((r) => r.method === 'POST' && r.url === `${v1}/environments/env_stg/archive`)
        .flush(ok(archived));
      await settle(fixture);
      flushDetails(archived, []);
      await settle(fixture);

      expect(el.textContent).toContain('This Environment is archived');
      expect(button(el, 'Restore Environment')).toBeDefined();
      expect(el.querySelector('#environment-name')).toBeNull();
    });

    it('adds a Target', async () => {
      const { fixture, el } = await open('admin');

      button(el, 'Add Target')!.click();
      await settle(fixture);
      type(el, '#target-name', 'staging-us-1');
      type(el, '#target-cluster', 'us-east-1');
      type(el, '#target-namespace', 'payments');
      el.querySelector('form[aria-label="Add a Target"]')!.dispatchEvent(new Event('submit'));
      await settle(fixture);

      const create = http.expectOne(
        (r) => r.method === 'POST' && r.url === `${v1}/environments/env_stg/targets`,
      );
      expect(create.request.body).toEqual({
        display_name: 'staging-us-1',
        runtime: 'kubernetes',
        locator: { cluster: 'us-east-1', namespace: 'payments' },
      });
      const added = {
        ...target,
        id: 'tgt_2',
        display_name: 'staging-us-1',
        locator: { cluster: 'us-east-1', namespace: 'payments' },
      };
      create.flush(ok(added));
      await settle(fixture);
      flushDetails(staging, [target, added]);
      await settle(fixture);

      expect(el.textContent).toContain('us-east-1 / payments');
      expect(el.querySelector('form[aria-label="Add a Target"]')).toBeNull();
    });

    it('changes and archives a Target', async () => {
      const { fixture, el } = await open('admin');

      el.querySelector<HTMLButtonElement>('button[aria-label="Change staging-eu-1"]')!.click();
      await settle(fixture);
      expect(el.querySelector<HTMLInputElement>('#edit-target-cluster')!.value).toBe('eu');
      type(el, '#edit-target-namespace', 'checkout');
      el.querySelector('form[aria-label="Change staging-eu-1"]')!.dispatchEvent(
        new Event('submit'),
      );
      await settle(fixture);
      const update = http.expectOne((r) => r.method === 'PATCH' && r.url === `${v1}/targets/tgt_1`);
      expect(update.request.body).toEqual({
        display_name: 'staging-eu-1',
        locator: { cluster: 'eu', namespace: 'checkout' },
      });
      const changed = { ...target, locator: { cluster: 'eu', namespace: 'checkout' } };
      update.flush(ok(changed));
      await settle(fixture);
      flushDetails(staging, [changed]);
      await settle(fixture);

      el.querySelector<HTMLButtonElement>('button[aria-label="Archive staging-eu-1"]')!.click();
      await settle(fixture);
      http
        .expectOne((r) => r.method === 'POST' && r.url === `${v1}/targets/tgt_1/archive`)
        .flush(ok({ ...changed, deleted_at: '2026-11-07T00:00:00Z' }));
      await settle(fixture);
      flushDetails(staging, [{ ...changed, deleted_at: '2026-11-07T00:00:00Z' }]);
      await settle(fixture);

      expect(el.querySelector('button[aria-label="Restore staging-eu-1"]')).not.toBeNull();
    });

    it('is read-only for members', async () => {
      const { el } = await open('member');

      expect(el.querySelector('#environment-name')).toBeNull();
      expect(button(el, 'Add Target')).toBeUndefined();
      expect(button(el, 'Archive…')).toBeUndefined();
      expect(el.querySelector('button[aria-label^="Change"]')).toBeNull();
    });
  });
});
