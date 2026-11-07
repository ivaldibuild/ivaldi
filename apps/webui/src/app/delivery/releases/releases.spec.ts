import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { Type } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideRouter, Router } from '@angular/router';
import { API_BASE_URL } from '../../api-base-url';
import { NewReleasePage } from './new-release-page';
import { ReleaseList } from './release-list';
import { ReleasePage } from './release-page';

const v1 = 'https://cp.test/v1';
const ok = <T>(data: T) => ({ status: 'success', data });
const page = <T>(items: T[]) =>
  ok({ items, total_count: items.length, has_next: false, next_cursor: null });
const digest = `sha256:${'4f9c2e1a7b30'.padEnd(64, '0')}`;
const releasesUrl = `${v1}/applications/app_1/releases?limit=20&sort_by=created_at&sort_order=desc`;

const application = {
  id: 'app_1',
  workspace_id: 'wsp_1',
  project_id: 'prj_1',
  owner_team_id: 'team_1',
  slug: 'payments-api',
  display_name: 'payments-api',
};
const channels = [
  { id: 'ch_main', display_name: 'Mainline', is_default: true, archived_at: null, rules: [] },
  { id: 'ch_hot', display_name: 'Hotfix', is_default: false, archived_at: null, rules: [] },
];
const artifacts = [
  {
    id: 'art_api',
    name: 'api',
    artifact_kind: 'container_image',
    artifact_name: 'ghcr.io/northwind/payments-api',
    required: true,
    archived_at: null,
  },
  {
    id: 'art_mig',
    name: 'migrate',
    artifact_kind: 'container_image',
    artifact_name: 'ghcr.io/northwind/payments-migrate',
    required: false,
    archived_at: null,
  },
];
const build = {
  id: 'bld_1',
  artifact_kind: 'container_image',
  artifact_name: 'ghcr.io/northwind/payments-api',
  digest,
  provenance: 'ci_reported',
  version: '1.4.0',
  commit_sha: '9e1f0c2d3b4a5968',
  source_ref: 'refs/tags/v1.4.0',
  ci_provider: 'github_actions',
  ci_run_ref: 'https://github.com/northwind/payments/actions/runs/1',
  built_at: '2026-11-06T10:00:00Z',
  reported_tags: [],
};
function release(overrides: Record<string, unknown> = {}) {
  return {
    id: 'rel_1',
    application_id: 'app_1',
    channel_id: 'ch_main',
    version: '1.4.0',
    status: 'published',
    source: 'created',
    created_at: '2026-11-06T10:05:00Z',
    published_at: '2026-11-06T10:06:00Z',
    withdrawn_at: null,
    builds: [{ artifact_id: 'art_api', build }],
    ...overrides,
  };
}

describe('releases', () => {
  let http: HttpTestingController;
  let navigate: ReturnType<typeof vi.spyOn>;

  function render<T>(component: Type<T>, inputs: Record<string, unknown>) {
    TestBed.configureTestingModule({
      imports: [component],
      providers: [
        provideHttpClient(),
        provideHttpClientTesting(),
        provideRouter([]),
        { provide: API_BASE_URL, useValue: 'https://cp.test' },
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

  const button = (el: HTMLElement, text: string) =>
    [...el.querySelectorAll('button')].find((b) => b.textContent?.includes(text));

  afterEach(() => http.verify());

  describe('the list on the Application page', () => {
    async function renderList() {
      const { fixture, el } = render(ReleaseList, {
        applicationId: 'app_1',
        channels,
        artifacts,
      });
      await settle(fixture);
      http
        .expectOne(releasesUrl)
        .flush(
          page([
            release({ id: 'rel_2', version: '1.5.0', status: 'draft', published_at: null }),
            release(),
          ]),
        );
      await settle(fixture);
      return { fixture, el };
    }

    it('lists Releases with their Channel, status and Builds', async () => {
      const { el } = await renderList();

      const rows = [...el.querySelectorAll('tbody tr')];
      expect(rows.map((r) => r.querySelector('th a')?.textContent?.trim())).toEqual([
        '1.5.0',
        '1.4.0',
      ]);
      expect(rows[0].textContent).toContain('Draft');
      expect(rows[0].textContent).toContain('Mainline');
      expect(rows[1].textContent).toContain('api');
      expect(rows[1].textContent).toContain('1.4.0');
      expect(rows[1].querySelector('th a')?.getAttribute('href')).toBe('/releases/rel_1');
      expect(button(el, 'Publish')?.getAttribute('aria-label')).toBe('Publish 1.5.0');
      expect(el.textContent).toContain('2 Releases');
    });

    it('publishes a draft and reloads', async () => {
      const { fixture, el } = await renderList();

      button(el, 'Publish')!.click();
      await settle(fixture);
      const req = http.expectOne(`${v1}/releases/rel_2/publish`);
      expect(req.request.method).toBe('POST');
      req.flush(ok(release({ id: 'rel_2', version: '1.5.0' })));
      await settle(fixture);
      http.expectOne(releasesUrl).flush(page([release({ id: 'rel_2', version: '1.5.0' })]));
      await settle(fixture);

      expect(button(el, 'Publish')).toBeUndefined();
    });

    it("says why a draft can't be published", async () => {
      const { fixture, el } = await renderList();

      button(el, 'Publish')!.click();
      await settle(fixture);
      http.expectOne(`${v1}/releases/rel_2/publish`).flush(
        {
          detail: 'the Release does not meet the Channel rules: source_ref must match refs/tags/*',
        },
        { status: 422, statusText: 'Unprocessable Entity' },
      );
      await settle(fixture);

      expect(el.querySelector('[role="alert"]')?.textContent).toContain(
        'The Release does not meet the Channel rules',
      );
    });

    it('filters by status', async () => {
      const { fixture, el } = await renderList();

      const select = el.querySelector<HTMLSelectElement>('#release-status')!;
      select.value = 'withdrawn';
      select.dispatchEvent(new Event('change'));
      await settle(fixture);
      http
        .expectOne(
          `${v1}/applications/app_1/releases?limit=20&sort_by=created_at&sort_order=desc&status=withdrawn`,
        )
        .flush(page([]));
      await settle(fixture);

      expect(el.textContent).toContain('No withdrawn Releases.');
    });
  });

  describe('creating a Release', () => {
    async function renderForm() {
      const { fixture, el } = render(NewReleasePage, { id: 'app_1' });
      await settle(fixture);
      http.expectOne(`${v1}/applications/app_1`).flush(ok(application));
      http.expectOne(`${v1}/applications/app_1/channels`).flush(ok(channels));
      http.expectOne(`${v1}/applications/app_1/artifacts`).flush(ok(artifacts));
      await settle(fixture);
      return { fixture, el };
    }

    function type(el: HTMLElement, selector: string, value: string): void {
      const input = el.querySelector<HTMLInputElement>(selector)!;
      input.value = value;
      input.dispatchEvent(new Event('input'));
      input.dispatchEvent(new Event('blur'));
    }

    it('creates and publishes a Release from a digest per artifact', async () => {
      const { fixture, el } = await renderForm();

      const labels = [...el.querySelectorAll('fieldset label')].map((l) =>
        l.textContent?.replace(/\s+/g, ' ').trim(),
      );
      expect(labels).toEqual(['api · required', 'migrate · optional']);
      type(el, '#release-version', '1.4.0');
      type(el, '#release-digest-0', digest);
      el.querySelector('#release-form')!.dispatchEvent(new Event('submit'));
      await settle(fixture);

      const req = http.expectOne(`${v1}/applications/app_1/releases`);
      expect(req.request.body).toEqual({
        version: '1.4.0',
        builds: [{ artifact: 'api', digest }],
        publish: true,
      });
      req.flush(ok(release()));
      await settle(fixture);
      expect(navigate).toHaveBeenCalledWith(['/releases', 'rel_1']);
    });

    it('checks the version and digests before calling the control plane', async () => {
      const { fixture, el } = await renderForm();

      type(el, '#release-digest-1', 'latest');
      el.querySelector('#release-form')!.dispatchEvent(new Event('submit'));
      await settle(fixture);

      expect(el.textContent).toContain('Give the version, such as 1.4.0.');
      expect(el.textContent).toContain('Give the digest of the Build to release.');
      expect(el.textContent).toContain('Use sha256: followed by 64 hexadecimal characters.');
      http.expectNone(`${v1}/applications/app_1/releases`);
    });
  });

  describe('the Release page', () => {
    async function renderRelease(overrides: Record<string, unknown> = {}) {
      const { fixture, el } = render(ReleasePage, { id: 'rel_1' });
      await settle(fixture);
      http.expectOne(`${v1}/releases/rel_1`).flush(ok(release(overrides)));
      await settle(fixture);
      http.expectOne(`${v1}/applications/app_1`).flush(ok(application));
      http.expectOne(`${v1}/applications/app_1/channels`).flush(ok(channels));
      http.expectOne(`${v1}/applications/app_1/artifacts`).flush(ok(artifacts));
      await settle(fixture);
      return { fixture, el };
    }

    it('shows its Channel and each Build in full', async () => {
      const { el } = await renderRelease();

      expect(el.querySelector('h1')?.textContent).toContain('1.4.0');
      expect(el.textContent).toContain('Published');
      expect(el.textContent).toContain('In Mainline');
      const row = el.querySelector('tbody tr')!;
      expect(row.textContent).toContain('api');
      expect(row.textContent).toContain(digest);
      expect(row.textContent).toContain('9e1f0c2');
      expect(row.textContent).toContain('refs/tags/v1.4.0');
      expect(row.querySelector('a')?.getAttribute('href')).toBe(build.ci_run_ref);
    });

    it('withdraws a published Release once confirmed', async () => {
      const { fixture, el } = await renderRelease();

      button(el, 'Withdraw')!.click();
      await settle(fixture);
      expect(el.querySelector('[role="alertdialog"]')?.textContent).toContain(
        "It won't be deployed again.",
      );
      button(el, 'Withdraw 1.4.0')!.click();
      await settle(fixture);
      http.expectOne(`${v1}/releases/rel_1/withdraw`).flush(ok(release({ status: 'withdrawn' })));
      await settle(fixture);

      http
        .expectOne(`${v1}/releases/rel_1`)
        .flush(ok(release({ status: 'withdrawn', withdrawn_at: '2026-11-06T12:00:00Z' })));
      await settle(fixture);
      http.expectOne(`${v1}/applications/app_1`).flush(ok(application));
      http.expectOne(`${v1}/applications/app_1/channels`).flush(ok(channels));
      http.expectOne(`${v1}/applications/app_1/artifacts`).flush(ok(artifacts));
      await settle(fixture);

      expect(el.querySelector('[role="alertdialog"]')).toBeNull();
      expect(el.textContent).toContain('Withdrawn');
      expect(button(el, 'Withdraw')).toBeUndefined();
    });

    it('publishes a draft', async () => {
      const { fixture, el } = await renderRelease({ status: 'draft', published_at: null });

      expect(el.textContent).toContain("A draft can't be deployed.");
      button(el, 'Publish')!.click();
      await settle(fixture);
      http.expectOne(`${v1}/releases/rel_1/publish`).flush(ok(release()));
      await settle(fixture);
      http.expectOne(`${v1}/releases/rel_1`).flush(ok(release()));
      await settle(fixture);
      http.expectOne(`${v1}/applications/app_1`).flush(ok(application));
      http.expectOne(`${v1}/applications/app_1/channels`).flush(ok(channels));
      http.expectOne(`${v1}/applications/app_1/artifacts`).flush(ok(artifacts));
      await settle(fixture);

      expect(button(el, 'Publish')).toBeUndefined();
    });
  });
});
