import { provideHttpClient, withInterceptors } from '@angular/common/http';
import { HttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { firstValueFrom } from 'rxjs';
import { API_BASE_URL } from '../api-base-url';
import { authInterceptor } from './auth';
import { BrowserLocation } from './browser-location';
import { Session, WEB_CLIENT_ID } from './session';

const cp = 'https://cp.test';

function jwt(claims: object): string {
  const part = (o: object) => btoa(JSON.stringify(o)).replace(/=+$/, '');
  return `${part({ alg: 'EdDSA' })}.${part(claims)}.sig`;
}

describe('Session', () => {
  let session: Session;
  let http: HttpTestingController;
  let assign: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    sessionStorage.clear();
    assign = vi.fn();
    TestBed.configureTestingModule({
      providers: [
        provideHttpClient(withInterceptors([authInterceptor])),
        provideHttpClientTesting(),
        { provide: API_BASE_URL, useValue: cp },
        { provide: BrowserLocation, useValue: { host: 'app.test', assign } },
      ],
    });
    session = TestBed.inject(Session);
    http = TestBed.inject(HttpTestingController);
  });

  afterEach(() => http.verify());

  async function signInWith(tokens: object): Promise<string> {
    await session.signIn('/setup/team');
    const url = new URL(assign.mock.calls[0][0]);
    const done = session.completeSignIn('CODE', url.searchParams.get('state')!);
    http.expectOne(`${cp}/oauth/token`).flush(tokens);
    return done;
  }

  it('starts an authorization code flow with PKCE', async () => {
    await session.signIn('/setup/team');

    const url = new URL(assign.mock.calls[0][0]);
    expect(url.origin + url.pathname).toBe(`${cp}/oauth/authorize`);
    expect(url.searchParams.get('client_id')).toBe(WEB_CLIENT_ID);
    expect(url.searchParams.get('response_type')).toBe('code');
    expect(url.searchParams.get('code_challenge_method')).toBe('S256');
    expect(url.searchParams.get('code_challenge')).toMatch(/^[\w-]{43}$/);
    expect(url.searchParams.get('redirect_uri')).toBe(`${location.origin}/auth/callback`);
  });

  it('exchanges the code and returns to the page that asked', async () => {
    const returnTo = await signInWith({
      access_token: jwt({ sub: 'prn_me', org: 'org_1' }),
      expires_in: 900,
      refresh_token: 'R1',
    });

    expect(returnTo).toBe('/setup/team');
    expect(session.principalId()).toBe('prn_me');
    expect(session.organizationId()).toBe('org_1');
    expect(await session.accessToken()).toContain('.');
  });

  it('refuses a callback this tab did not start', async () => {
    await session.signIn('/setup');
    await expect(session.completeSignIn('CODE', 'someone-else')).rejects.toThrow(
      'not started by this browser tab',
    );
  });

  it('refreshes an access token that is about to expire', async () => {
    await signInWith({ access_token: jwt({ sub: 'prn_me' }), expires_in: 10, refresh_token: 'R1' });

    const token = session.accessToken();
    const req = http.expectOne(`${cp}/oauth/token`);
    expect(req.request.body).toContain('grant_type=refresh_token');
    expect(req.request.body).toContain('refresh_token=R1');
    req.flush({ access_token: jwt({ sub: 'prn_me', v: 2 }), expires_in: 900, refresh_token: 'R2' });

    expect(await token).toBe(jwt({ sub: 'prn_me', v: 2 }));
  });

  it('adds the access token to API calls but not to the public sign-in ones', async () => {
    const access = jwt({ sub: 'prn_me' });
    await signInWith({ access_token: access, expires_in: 900 });
    const client = TestBed.inject(HttpClient);

    void firstValueFrom(client.get(`${cp}/v1/teams`));
    void firstValueFrom(client.get(`${cp}/v1/auth/login`));
    await new Promise((resolve) => setTimeout(resolve));

    expect(http.expectOne(`${cp}/v1/teams`).request.headers.get('Authorization')).toBe(
      `Bearer ${access}`,
    );
    expect(http.expectOne(`${cp}/v1/auth/login`).request.headers.has('Authorization')).toBe(false);
  });
});
