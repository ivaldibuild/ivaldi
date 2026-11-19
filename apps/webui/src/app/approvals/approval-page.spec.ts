import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { API_BASE_URL } from '../api-base-url';
import { ApprovalPage } from './approval-page';

const v1 = 'https://cp.test/v1';
const ok = <T>(data: T) => ({ status: 'success', data });

const request = (overrides: Record<string, unknown> = {}) => ({
  id: 'apr_1',
  title: 'Deploy payments-api 1.5.0 to Production / prod-eu-1',
  state: 'pending',
  can_decide: true,
  subject_type: 'delivery.deployment',
  subject_id: 'dep_1',
  reason: 'agent Sessions need a person to approve their Deployments',
  requested_by_principal_id: 'prn_session',
  on_behalf_of_principal_id: 'prn_ada',
  closed_by_principal_id: null,
  closed_at: null,
  note: null,
  created_at: '2026-11-19T09:00:00Z',
  summary: {
    application_id: 'app_1',
    release_id: 'rel_2',
    release_version: '1.5.0',
    environment_id: 'env_prd',
    environment_tier: 'production',
    target_id: 'tgt_prd',
    reason: 'rollout',
    restores_deployment_id: null,
    secrets_latest: false,
  },
  ...overrides,
});

describe('ApprovalPage', () => {
  let http: HttpTestingController;

  async function settle(fixture: ComponentFixture<unknown>): Promise<void> {
    for (let i = 0; i < 4; i++) {
      await new Promise((resolve) => setTimeout(resolve));
      fixture.detectChanges();
    }
  }

  /** Renders the page and answers what it reads about `fixtureRequest`. */
  async function render(fixtureRequest = request()) {
    TestBed.configureTestingModule({
      imports: [ApprovalPage],
      providers: [
        provideHttpClient(),
        provideHttpClientTesting(),
        provideRouter([]),
        { provide: API_BASE_URL, useValue: 'https://cp.test' },
      ],
    });
    http = TestBed.inject(HttpTestingController);
    const fixture = TestBed.createComponent(ApprovalPage);
    fixture.componentRef.setInput('id', 'apr_1');
    fixture.detectChanges();
    await settle(fixture);
    await answer(fixture, fixtureRequest);
    return { fixture, el: fixture.nativeElement as HTMLElement };
  }

  async function answer(fixture: ComponentFixture<unknown>, fixtureRequest: unknown) {
    http.expectOne(`${v1}/approvals/apr_1`).flush(ok(fixtureRequest));
    await settle(fixture);
    http
      .expectOne((r) => r.url.startsWith(`${v1}/principals`))
      .flush(
        ok([
          {
            id: 'prn_session',
            kind: 'agent_session',
            display_name: 'ses_1',
            status: 'active',
            teams: [],
          },
          { id: 'prn_ada', kind: 'user', display_name: 'Ada', status: 'active', teams: [] },
          { id: 'prn_zoe', kind: 'user', display_name: 'Zoe', status: 'active', teams: [] },
        ]),
      );
    http
      .expectOne(`${v1}/applications/app_1`)
      .flush(ok({ id: 'app_1', display_name: 'payments-api' }));
    http
      .expectOne(`${v1}/environments/env_prd`)
      .flush(ok({ id: 'env_prd', display_name: 'Production', tier: 'production' }));
    http
      .expectOne((r) => r.url.startsWith(`${v1}/environments/env_prd/targets`))
      .flush(
        ok({
          items: [{ id: 'tgt_prd', display_name: 'prod-eu-1' }],
          total_count: 1,
          has_next: false,
          next_cursor: null,
        }),
      );
    await settle(fixture);
  }

  const button = (el: HTMLElement, text: string) =>
    [...el.querySelectorAll('button')].find((b) => b.textContent?.includes(text))!;

  function typeNote(fixture: ComponentFixture<unknown>, el: HTMLElement, note: string) {
    const textarea = el.querySelector('textarea')!;
    textarea.value = note;
    textarea.dispatchEvent(new Event('input'));
    fixture.detectChanges();
  }

  afterEach(() => http.verify());

  it('shows what waits and who asked, and approves it with a note', async () => {
    const { fixture, el } = await render();

    expect(el.querySelector('h1')?.textContent).toContain('Deploy payments-api 1.5.0');
    expect(el.textContent).toContain('An agent Session, acting for Ada');
    const facts = el.querySelector('dl')!.textContent!;
    expect(facts).toContain('Production (production)');
    expect(facts).toContain('prod-eu-1');
    expect(el.querySelector('a[href="/deployments/dep_1"]')?.textContent).toContain(
      'View the Deployment',
    );

    typeNote(fixture, el, ' ship it ');
    button(el, 'Approve').click();
    await settle(fixture);
    const approve = http.expectOne(`${v1}/approvals/apr_1/approve`);
    expect(approve.request.body).toEqual({ note: 'ship it' });
    approve.flush(ok(request({ state: 'approved' })));
    await settle(fixture);
    http.expectOne(`${v1}/notifications/unread-count`).flush(ok({ count: 0 }));

    await answer(
      fixture,
      request({
        state: 'approved',
        closed_by_principal_id: 'prn_zoe',
        closed_at: '2026-11-19T09:05:00Z',
        note: 'ship it',
      }),
    );
    expect(el.textContent).toContain('Approved by Zoe');
    expect(el.querySelector('textarea')).toBeNull();
  });

  it('asks why before rejecting', async () => {
    const { fixture, el } = await render();

    button(el, 'Reject').click();
    await settle(fixture);
    expect(el.textContent).toContain("Say why you're rejecting it.");
    http.expectNone(`${v1}/approvals/apr_1/reject`);

    typeNote(fixture, el, 'not during the freeze');
    button(el, 'Reject').click();
    await settle(fixture);
    const reject = http.expectOne(`${v1}/approvals/apr_1/reject`);
    expect(reject.request.body).toEqual({ note: 'not during the freeze' });
    reject.flush(ok(request({ state: 'rejected' })));
    await settle(fixture);
    http.expectOne(`${v1}/notifications/unread-count`).flush(ok({ count: 0 }));
    await answer(fixture, request({ state: 'rejected' }));
  });

  it('offers no decision to someone who cannot make it', async () => {
    const { el } = await render(request({ can_decide: false }));

    expect(el.querySelector('textarea')).toBeNull();
    expect(el.textContent).toContain("You can't decide this request.");
  });
});
