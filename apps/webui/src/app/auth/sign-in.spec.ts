import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { ActivatedRoute, convertToParamMap, Params } from '@angular/router';
import { API_BASE_URL } from '../api-base-url';
import { BrowserLocation } from './browser-location';
import { SignIn } from './sign-in';
import { LoginRequest } from './sign-in-api';

const api = 'https://cp.test/v1/auth';

const loginRequest: LoginRequest = {
  client_id: 'web',
  client_name: 'Ivaldi web',
  password: true,
  sign_up: false,
  providers: [
    { kind: 'github', name: 'GitHub', start_url: 'https://cp.test/auth/oauth/github/start?c=LC' },
  ],
};

describe('SignIn', () => {
  let http: HttpTestingController;
  let assign: ReturnType<typeof vi.fn>;

  async function render(params: Params): Promise<ComponentFixture<SignIn>> {
    assign = vi.fn();
    TestBed.configureTestingModule({
      imports: [SignIn],
      providers: [
        provideHttpClient(),
        provideHttpClientTesting(),
        { provide: API_BASE_URL, useValue: 'https://cp.test' },
        { provide: BrowserLocation, useValue: { host: 'cp.test', assign } },
        {
          provide: ActivatedRoute,
          useValue: { snapshot: { queryParamMap: convertToParamMap(params) } },
        },
      ],
    });
    http = TestBed.inject(HttpTestingController);
    const fixture = TestBed.createComponent(SignIn);
    fixture.detectChanges();
    return fixture;
  }

  async function settle(fixture: ComponentFixture<SignIn>): Promise<HTMLElement> {
    // Let the promise chain behind a flushed request finish before rendering.
    await new Promise((resolve) => setTimeout(resolve));
    await fixture.whenStable();
    fixture.detectChanges();
    return fixture.nativeElement as HTMLElement;
  }

  async function renderForm(params: Params = { login_challenge: 'LC' }) {
    const fixture = await render(params);
    http.expectOne(`${api}/login?login_challenge=LC`).flush(loginRequest);
    return { fixture, el: await settle(fixture) };
  }

  function type(el: HTMLElement, selector: string, value: string): void {
    const input = el.querySelector<HTMLInputElement>(selector)!;
    input.value = value;
    input.dispatchEvent(new Event('input'));
    input.dispatchEvent(new Event('blur'));
  }

  async function submitPassword(fixture: ComponentFixture<SignIn>, el: HTMLElement) {
    type(el, '#sign-in-email', 'ama@northwind.dev');
    type(el, '#sign-in-password', 'hunter2');
    el.querySelector('form')!.dispatchEvent(new Event('submit'));
    await settle(fixture);
  }

  afterEach(() => http.verify());

  it('offers the providers and the password form from the login request', async () => {
    const { el } = await renderForm();

    expect(el.querySelector('h1')?.textContent).toContain('Sign in');
    expect(el.textContent).toContain('to continue to Ivaldi web.');
    const provider = el.querySelector<HTMLAnchorElement>('a[href*="github"]');
    expect(provider?.textContent).toContain('Continue with GitHub');
    expect(provider?.getAttribute('href')).toBe(loginRequest.providers[0].start_url);
    expect(el.querySelector('#sign-in-email')).not.toBeNull();
    expect(el.querySelector('#sign-in-password')).not.toBeNull();
  });

  it('sends the browser on when the password step answers with redirect_to', async () => {
    const { fixture, el } = await renderForm();

    await submitPassword(fixture, el);
    const req = http.expectOne(`${api}/login/password`);
    expect(req.request.body).toEqual({
      login_challenge: 'LC',
      email: 'ama@northwind.dev',
      password: 'hunter2',
    });
    req.flush({ redirect_to: 'https://cp.test/oauth/authorize/continue?login_verifier=V' });
    await settle(fixture);

    expect(assign).toHaveBeenCalledWith(
      'https://cp.test/oauth/authorize/continue?login_verifier=V',
    );
    expect(el.textContent).toContain('Taking you back');
  });

  it('shows a notice and clears the password when the credentials are wrong', async () => {
    const { fixture, el } = await renderForm();

    await submitPassword(fixture, el);
    http
      .expectOne(`${api}/login/password`)
      .flush({ title: 'Unauthorized' }, { status: 401, statusText: 'Unauthorized' });
    await settle(fixture);

    expect(el.querySelector('[role="alert"]')?.textContent).toContain("That didn't work");
    expect(el.querySelector<HTMLInputElement>('#sign-in-password')?.value).toBe('');
    expect(el.textContent).not.toContain('Enter your password.');
    expect(assign).not.toHaveBeenCalled();
  });

  it('checks the fields before calling the control plane', async () => {
    const { fixture, el } = await renderForm();

    el.querySelector('form')!.dispatchEvent(new Event('submit'));
    await settle(fixture);

    expect(el.textContent).toContain('Enter your email.');
    expect(el.textContent).toContain('Enter your password.');
    http.expectNone(`${api}/login/password`);
  });

  it('lets the person choose an organisation when they belong to several', async () => {
    const { fixture, el } = await renderForm();

    await submitPassword(fixture, el);
    http.expectOne(`${api}/login/password`).flush({
      login_session: 'LS',
      organizations: [
        { id: 'org1', slug: 'northwind', name: 'Northwind' },
        { id: 'org2', slug: 'contoso', name: 'Contoso' },
      ],
    });
    await settle(fixture);

    expect(el.querySelector('h1')?.textContent).toContain('Choose an organisation');
    const contoso = [...el.querySelectorAll('button')].find((b) =>
      b.textContent?.includes('Contoso'),
    )!;
    contoso.click();
    await settle(fixture);

    const req = http.expectOne(`${api}/login/organization`);
    expect(req.request.body).toEqual({
      login_challenge: 'LC',
      login_session: 'LS',
      organization_id: 'org2',
    });
    req.flush({ redirect_to: 'https://cp.test/done' });
    await settle(fixture);
    expect(assign).toHaveBeenCalledWith('https://cp.test/done');
  });

  it('lists the organisations a provider sign-in came back to choose from', async () => {
    const fixture = await render({ login_challenge: 'LC', login_session: 'LS' });
    const req = http.expectOne(`${api}/login/organizations`);
    expect(req.request.body).toEqual({ login_challenge: 'LC', login_session: 'LS' });
    req.flush({
      login_session: 'LS2',
      organizations: [
        { id: 'org1', slug: 'northwind', name: 'Northwind' },
        { id: 'org2', slug: 'contoso', name: 'Contoso' },
      ],
    });
    const el = await settle(fixture);

    expect(el.querySelector('h1')?.textContent).toContain('Choose an organisation');
    [...el.querySelectorAll('button')].find((b) => b.textContent?.includes('Northwind'))!.click();
    await settle(fixture);

    expect(http.expectOne(`${api}/login/organization`).request.body).toEqual({
      login_challenge: 'LC',
      login_session: 'LS2',
      organization_id: 'org1',
    });
  });

  it('offers another way when the login session has no organisation left', async () => {
    const fixture = await render({ login_challenge: 'LC', login_session: 'LS' });
    http
      .expectOne(`${api}/login/organizations`)
      .flush({ title: 'Forbidden' }, { status: 403, statusText: 'Forbidden' });
    await settle(fixture);
    http.expectOne(`${api}/login?login_challenge=LC`).flush(loginRequest);
    const el = await settle(fixture);

    expect(el.querySelector('[role="alert"]')?.textContent).toContain(
      "This account can't sign in here",
    );
    expect(el.querySelector('#sign-in-email')).not.toBeNull();
  });

  it('explains a provider refusal above the form', async () => {
    const { el } = await renderForm({ login_challenge: 'LC', error: 'not_admitted' });

    expect(el.querySelector('[role="alert"]')?.textContent).toContain(
      "This account isn't allowed here",
    );
    expect(el.querySelector('#sign-in-email')).not.toBeNull();
  });

  it('asks the person to start again when the challenge has expired', async () => {
    const fixture = await render({ login_challenge: 'LC' });
    http
      .expectOne(`${api}/login?login_challenge=LC`)
      .flush({ title: 'Bad Request' }, { status: 400, statusText: 'Bad Request' });
    const el = await settle(fixture);

    expect(el.querySelector('h1')?.textContent).toContain('This sign-in has timed out');
    expect(el.querySelector('form')).toBeNull();
  });

  it('says there is nothing to continue without a login challenge', async () => {
    const fixture = await render({});
    const el = await settle(fixture);

    expect(el.querySelector('h1')?.textContent).toContain('Open the application you want to use');
    http.expectNone(() => true);
  });

  it('confirms a sign-out', async () => {
    const fixture = await render({ logged_out: 'true' });
    const el = await settle(fixture);

    expect(el.querySelector('h1')?.textContent).toContain("You've signed out");
  });
});
