import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { signal } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { CurrentUser } from '../api/current-user';
import { API_BASE_URL } from '../api-base-url';
import { WorkspaceContext } from '../delivery/workspace-context';
import { ServiceAccountPage } from './service-account-page';

const v1 = 'https://cp.test/v1';
const ok = <T>(data: T) => ({ status: 'success', data });
const page = <T>(items: T[]) =>
  ok({ items, total_count: items.length, has_next: false, next_cursor: null });

const account = {
  id: 'prn_gha',
  display_name: 'GitHub Actions',
  description: 'Creates Releases from tags',
  status: 'active',
  created_by_principal_id: 'prn_ama',
  created_at: '2026-11-01T10:00:00Z',
  updated_at: '2026-11-01T10:00:00Z',
};
const platform = { id: 'wsp_1', slug: 'platform', display_name: 'Platform', is_default: true };
const token = {
  id: 'tok_1',
  organization_id: 'org_1',
  principal_id: 'prn_gha',
  name: 'github-actions',
  token_prefix: 'ivt_Ab3dEf9h',
  expires_at: null,
  last_used_at: '2026-11-05T09:30:00Z',
  revoked_at: null,
  created_at: '2026-11-01T10:00:00Z',
};
const adminOnPlatform = {
  id: 'gr_1',
  role: 'admin',
  resource_id: 'wsp_1',
  principal: { id: 'prn_gha', kind: 'service_account', display_name: 'GitHub Actions' },
  group: null,
  expires_at: null,
};
// Someone else's grant on the Organisation, which the page leaves out.
const someoneElse = {
  ...adminOnPlatform,
  id: 'gr_2',
  resource_id: 'org_1',
  principal: { id: 'prn_ama' },
};

describe('ServiceAccountPage', () => {
  let http: HttpTestingController;

  async function render(
    role: string,
    grants = [adminOnPlatform],
  ): Promise<{ fixture: ComponentFixture<ServiceAccountPage>; el: HTMLElement }> {
    TestBed.configureTestingModule({
      imports: [ServiceAccountPage],
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
        {
          provide: WorkspaceContext,
          useValue: {
            load: () => Promise.resolve(),
            workspaces: signal([platform]),
            current: signal(platform),
          },
        },
      ],
    });
    http = TestBed.inject(HttpTestingController);
    const fixture = TestBed.createComponent(ServiceAccountPage);
    fixture.componentRef.setInput('id', 'prn_gha');
    fixture.detectChanges();
    await settle(fixture);
    http.expectOne(`${v1}/service-accounts/prn_gha`).flush(ok(account));
    http.expectOne(`${v1}/service-accounts/prn_gha/tokens`).flush(ok([token]));
    await settle(fixture);
    flushGrants(grants);
    await settle(fixture);
    return { fixture, el: fixture.nativeElement as HTMLElement };
  }

  function flushGrants(grants: (typeof adminOnPlatform)[]): void {
    for (const resource of ['org_1', 'wsp_1']) {
      http
        .expectOne((r) => r.url.startsWith(`${v1}/grants?resource_id=${resource}&`))
        .flush(page([...grants, someoneElse].filter((g) => g.resource_id === resource)));
    }
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

  afterEach(() => http.verify());

  it('shows its roles and tokens', async () => {
    const { el } = await render('admin');

    expect(text(el.querySelector('h1'))).toBe('GitHub Actions');
    const access = [...el.querySelectorAll('section[aria-labelledby="access-heading"] li')];
    expect(access.map(text)).toEqual(['Admin on Workspace Platform Remove']);

    const row = text(el.querySelector('tbody tr'));
    expect(row).toContain('github-actions');
    expect(row).toContain('ivt_Ab3dEf9h…');
    expect(row).toContain('active');
    expect(row).toContain('Never');
    expect(row).toContain('5 Nov 2026');
  });

  it('gives it a role on the current Workspace by default', async () => {
    const { fixture, el } = await render('owner', []);

    expect(el.textContent).toContain("It has no roles yet, so it can't do anything.");
    expect(
      [...el.querySelectorAll<HTMLOptionElement>('#grant-role option')].map((o) => o.value),
    ).toEqual(['admin', 'member', 'viewer']);
    button(el, 'Give role')!.click();
    await settle(fixture);
    const create = http.expectOne((r) => r.method === 'POST' && r.url === `${v1}/grants`);
    expect(create.request.body).toEqual({
      principal_id: 'prn_gha',
      role: 'admin',
      resource_id: 'wsp_1',
    });
    create.flush(ok(adminOnPlatform));
    await settle(fixture);
    flushGrants([adminOnPlatform]);
    await settle(fixture);

    expect(el.textContent).toContain('Admin on Workspace Platform');
  });

  it('issues a token and shows its secret once', async () => {
    const { fixture, el } = await render('admin');
    vi.useFakeTimers({ toFake: ['Date'] });
    vi.setSystemTime(new Date('2026-11-07T12:00:00Z'));

    button(el, 'Issue token')!.click();
    await settle(fixture);
    const name = el.querySelector<HTMLInputElement>('#token-name')!;
    name.value = 'gitlab';
    name.dispatchEvent(new Event('input'));
    el.querySelector('form[aria-label="Issue a token"]')!.dispatchEvent(new Event('submit'));
    await settle(fixture);

    const issue = http.expectOne(
      (r) => r.method === 'POST' && r.url === `${v1}/service-accounts/prn_gha/tokens`,
    );
    expect(issue.request.body).toEqual({ name: 'gitlab', expires_at: '2027-02-05T12:00:00.000Z' });
    issue.flush(
      ok({
        secret: 'ivt_s3cr3t',
        token: {
          ...token,
          id: 'tok_2',
          name: 'gitlab',
          token_prefix: 'ivt_s3cr3t',
          expires_at: '2027-02-05T12:00:00Z',
        },
      }),
    );
    await settle(fixture);
    vi.useRealTimers();

    expect(el.querySelector('[aria-label="The token\'s secret"]')?.textContent?.trim()).toBe(
      'ivt_s3cr3t',
    );
    expect(el.querySelectorAll('tbody tr').length).toBe(2);

    button(el, "I've stored it")!.click();
    await settle(fixture);
    expect(el.querySelector('[aria-label="The token\'s secret"]')).toBeNull();
  });

  it('revokes a token', async () => {
    const { fixture, el } = await render('admin');

    el.querySelector<HTMLButtonElement>('button[aria-label="Revoke github-actions"]')!.click();
    await settle(fixture);
    http
      .expectOne((r) => r.method === 'POST' && r.url === `${v1}/api-tokens/tok_1/revoke`)
      .flush(ok({ ...token, revoked_at: '2026-11-07T12:00:00Z' }));
    await settle(fixture);

    expect(text(el.querySelector('tbody tr'))).toContain('revoked');
    expect(el.querySelector('button[aria-label="Revoke github-actions"]')).toBeNull();
  });

  it('suspends it', async () => {
    const { fixture, el } = await render('admin');

    button(el, 'Suspend')!.click();
    await settle(fixture);
    http
      .expectOne((r) => r.method === 'POST' && r.url === `${v1}/service-accounts/prn_gha/suspend`)
      .flush(ok({ ...account, status: 'suspended' }));
    await settle(fixture);

    expect(button(el, 'Reactivate')).toBeDefined();
    expect(el.textContent).toContain("While it's suspended, none of its tokens work.");
  });

  it('is read-only for members', async () => {
    const { el } = await render('member');

    expect(button(el, 'Suspend')).toBeUndefined();
    expect(button(el, 'Issue token')).toBeUndefined();
    expect(button(el, 'Give role')).toBeUndefined();
    expect(el.querySelector('button[aria-label^="Revoke"]')).toBeNull();
  });
});
