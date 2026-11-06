import { HttpClient, HttpHeaders } from '@angular/common/http';
import { computed, DOCUMENT, inject, Service, signal } from '@angular/core';
import { firstValueFrom } from 'rxjs';
import { API_BASE_URL } from '../api-base-url';
import { BrowserLocation } from './browser-location';
import { codeChallenge, randomToken } from './pkce';

/** The web app's OAuth client, which the control plane registers on start. */
export const WEB_CLIENT_ID = 'ivaldi-web';

const pendingKey = 'ivaldi.sign-in';
/** Refresh this long before the access token expires. */
const refreshMargin = 30_000;

interface PendingSignIn {
  state: string;
  verifier: string;
  returnTo: string;
}

interface TokenResponse {
  access_token: string;
  expires_in: number;
  refresh_token?: string;
}

interface Tokens {
  access: string;
  refresh?: string;
  expiresAt: number;
  principalId: string;
  organizationId: string | null;
}

/**
 * The person's sign-in to the control plane, as the `ivaldi-web` OAuth client: the
 * authorization code flow with PKCE (docs/self-hosting/sign-in.md).
 *
 * Tokens live only in memory, so a reload starts a new authorization. The control plane
 * remembers who signed in, so that passes straight back without asking again.
 */
@Service()
export class Session {
  private readonly http = inject(HttpClient);
  private readonly base = inject(API_BASE_URL);
  private readonly location = inject(BrowserLocation);
  private readonly document = inject(DOCUMENT);

  private readonly tokens = signal<Tokens | null>(null);
  private refreshing: Promise<void> | null = null;

  readonly principalId = computed(() => this.tokens()?.principalId ?? null);
  readonly organizationId = computed(() => this.tokens()?.organizationId ?? null);

  private get redirectUri(): string {
    return `${this.document.location.origin}/auth/callback`;
  }

  /** Sends the browser to the control plane to sign in, then back to `returnTo`. */
  async signIn(returnTo: string): Promise<void> {
    const pending: PendingSignIn = { state: randomToken(), verifier: randomToken(), returnTo };
    this.storage()?.setItem(pendingKey, JSON.stringify(pending));
    const query = new URLSearchParams({
      response_type: 'code',
      client_id: WEB_CLIENT_ID,
      redirect_uri: this.redirectUri,
      state: pending.state,
      code_challenge: await codeChallenge(pending.verifier),
      code_challenge_method: 'S256',
    });
    this.location.assign(`${this.base}/oauth/authorize?${query}`);
  }

  /** Exchanges the code the control plane sent back, and returns where to go next. */
  async completeSignIn(code: string, state: string): Promise<string> {
    const storage = this.storage();
    const raw = storage?.getItem(pendingKey);
    storage?.removeItem(pendingKey);
    const pending = raw ? (JSON.parse(raw) as PendingSignIn) : null;
    if (!pending || pending.state !== state) {
      throw new Error('This sign-in was not started by this browser tab.');
    }
    await this.requestTokens({
      grant_type: 'authorization_code',
      code,
      redirect_uri: this.redirectUri,
      code_verifier: pending.verifier,
    });
    return pending.returnTo.startsWith('/') ? pending.returnTo : '/';
  }

  /** A current access token, refreshed if it is about to expire, or null when signed out. */
  async accessToken(): Promise<string | null> {
    const tokens = this.tokens();
    if (!tokens) {
      return null;
    }
    if (Date.now() < tokens.expiresAt - refreshMargin) {
      return tokens.access;
    }
    if (!tokens.refresh) {
      this.tokens.set(null);
      return null;
    }
    this.refreshing ??= this.requestTokens({
      grant_type: 'refresh_token',
      refresh_token: tokens.refresh,
    })
      .catch(() => this.tokens.set(null))
      .finally(() => (this.refreshing = null));
    await this.refreshing;
    return this.tokens()?.access ?? null;
  }

  private async requestTokens(params: Record<string, string>): Promise<void> {
    const body = new URLSearchParams({ ...params, client_id: WEB_CLIENT_ID }).toString();
    const response = await firstValueFrom(
      this.http.post<TokenResponse>(`${this.base}/oauth/token`, body, {
        headers: new HttpHeaders({ 'Content-Type': 'application/x-www-form-urlencoded' }),
      }),
    );
    const claims = decodeClaims(response.access_token);
    this.tokens.set({
      access: response.access_token,
      refresh: response.refresh_token,
      expiresAt: Date.now() + response.expires_in * 1000,
      principalId: claims.sub,
      organizationId: claims.org ?? null,
    });
  }

  private storage(): Storage | null {
    try {
      return this.document.defaultView?.sessionStorage ?? null;
    } catch {
      return null;
    }
  }
}

/** Reads an access token's claims. The control plane verifies the token; this only displays them. */
function decodeClaims(token: string): { sub: string; org?: string } {
  const payload = token.split('.')[1] ?? '';
  const json = atob(payload.replace(/-/g, '+').replace(/_/g, '/'));
  return JSON.parse(json) as { sub: string; org?: string };
}
