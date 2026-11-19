import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideRouter, Router } from '@angular/router';
import { API_BASE_URL } from '../api-base-url';
import { InboxPage } from './inbox-page';

const v1 = 'https://cp.test/v1';
const ok = <T>(data: T) => ({ status: 'success', data });
const page = <T>(items: T[]) =>
  ok({ items, total_count: items.length, has_next: false, next_cursor: null });

const notification = (overrides: Record<string, unknown> = {}) => ({
  id: 'ntf_1',
  kind: 'approval_requested',
  title: 'Deploy payments-api 1.5.0 to Production / prod-eu-1',
  body: 'An agent Session acting for Ada asked',
  link: '/approvals/apr_1',
  read_at: null,
  resolved_at: null,
  created_at: '2026-11-19T09:00:00Z',
  ...overrides,
});

describe('InboxPage', () => {
  let http: HttpTestingController;

  async function settle(fixture: ComponentFixture<unknown>): Promise<void> {
    for (let i = 0; i < 4; i++) {
      await new Promise((resolve) => setTimeout(resolve));
      fixture.detectChanges();
    }
  }

  async function render(items: unknown[], unread: number) {
    TestBed.configureTestingModule({
      imports: [InboxPage],
      providers: [
        provideHttpClient(),
        provideHttpClientTesting(),
        provideRouter([]),
        { provide: API_BASE_URL, useValue: 'https://cp.test' },
      ],
    });
    http = TestBed.inject(HttpTestingController);
    vi.spyOn(TestBed.inject(Router), 'navigateByUrl').mockResolvedValue(true);
    const fixture = TestBed.createComponent(InboxPage);
    fixture.detectChanges();
    http.expectOne((r) => r.url.startsWith(`${v1}/notifications?`)).flush(page(items));
    http.expectOne(`${v1}/notifications/unread-count`).flush(ok({ count: unread }));
    await settle(fixture);
    return { fixture, el: fixture.nativeElement as HTMLElement };
  }

  afterEach(() => http.verify());

  it('marks a notification read as it opens, and says when an ask no longer waits', async () => {
    const { fixture, el } = await render(
      [
        notification(),
        notification({
          id: 'ntf_2',
          read_at: '2026-11-19T08:00:00Z',
          resolved_at: '2026-11-19T08:30:00Z',
        }),
      ],
      1,
    );

    const links = [...el.querySelectorAll('ul[aria-label="Notifications"] a')];
    expect(links[0].getAttribute('href')).toBe('/approvals/apr_1');
    expect(links[0].textContent).toContain('Unread');
    expect(links[1].textContent).toContain('No longer waiting');

    (links[0] as HTMLElement).click();
    await settle(fixture);
    http.expectOne(`${v1}/notifications/ntf_1/read`).flush(ok(notification({ read_at: 'now' })));
    await settle(fixture);
    http.expectOne(`${v1}/notifications/unread-count`).flush(ok({ count: 0 }));

    (links[1] as HTMLElement).click();
    await settle(fixture);
    http.expectNone(`${v1}/notifications/ntf_2/read`);
  });

  it('marks them all read', async () => {
    const { fixture, el } = await render([notification()], 1);

    [...el.querySelectorAll('button')].find((b) => b.textContent?.includes('Mark all'))!.click();
    await settle(fixture);
    http.expectOne(`${v1}/notifications/read`).flush(ok(null));
    await settle(fixture);
    http
      .expectOne((r) => r.url.startsWith(`${v1}/notifications?`))
      .flush(page([notification({ read_at: '2026-11-19T09:01:00Z' })]));
    await settle(fixture);

    expect(el.querySelector('ul[aria-label="Notifications"]')?.textContent).not.toContain('Unread');
    const markAll = [...el.querySelectorAll('button')].find((b) =>
      b.textContent?.includes('Mark all'),
    );
    expect(markAll?.disabled).toBe(true);
  });
});
