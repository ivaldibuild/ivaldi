import { createHash, randomBytes } from 'node:crypto';
import { existsSync, readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { type Page } from '@playwright/test';

/**
 * The local stack the tests run against: the web app and the control plane started by
 * `make local_stack` (or `make local_serve` with `ng serve`). Override the addresses with
 * IVALDI_E2E_WEB_URL and IVALDI_E2E_API_URL.
 */
export const webURL = process.env['IVALDI_E2E_WEB_URL'] ?? 'http://localhost:3000';
export const apiURL = process.env['IVALDI_E2E_API_URL'] ?? 'http://localhost:8660';

/** The web app's first-party OAuth client and the redirect URI config.local.yml registers. */
export const clientId = 'ivaldi-web';
export const redirectURI = `${webURL}/auth/callback`;

const repoRoot = resolve(__dirname, '../../../..');

/**
 * The bootstrap administrator: IVALDI_E2E_ADMIN_EMAIL and IVALDI_E2E_ADMIN_PASSWORD, or else
 * bootstrap.admin_email from config.local.yml and the password `make local_secrets` wrote to
 * .local/admin-password. Undefined when neither is available.
 */
export function adminCredentials(): { email: string; password: string } | undefined {
  const email = process.env['IVALDI_E2E_ADMIN_EMAIL'] ?? configValue('admin_email');
  const password = process.env['IVALDI_E2E_ADMIN_PASSWORD'] ?? fileContents('.local/admin-password');
  return email && password ? { email, password } : undefined;
}

function configValue(key: string): string | undefined {
  const config = fileContents('config.local.yml');
  const match = config?.match(new RegExp(`^\\s*${key}:\\s*["']?([^"'#\\s]+)`, 'm'));
  return match?.[1];
}

function fileContents(path: string): string | undefined {
  const file = resolve(repoRoot, path);
  return existsSync(file) ? readFileSync(file, 'utf8').trim() : undefined;
}

/** A PKCE verifier and its S256 challenge. */
export interface Pkce {
  verifier: string;
  challenge: string;
}

export function newPkce(): Pkce {
  const verifier = randomBytes(32).toString('base64url');
  const challenge = createHash('sha256').update(verifier).digest('base64url');
  return { verifier, challenge };
}

/**
 * Starts a sign-in as the web app's client would: the browser opens the authorization
 * endpoint, and the control plane sends it to the sign-in page with a login challenge.
 */
export async function startSignIn(page: Page): Promise<{ pkce: Pkce; state: string }> {
  const pkce = newPkce();
  const state = randomBytes(8).toString('hex');
  const authorize = new URL('/oauth/authorize', apiURL);
  authorize.search = new URLSearchParams({
    response_type: 'code',
    client_id: clientId,
    redirect_uri: redirectURI,
    state,
    code_challenge: pkce.challenge,
    code_challenge_method: 'S256',
  }).toString();

  await page.goto(authorize.toString());
  await page.waitForURL((url) => url.pathname === '/login' && url.searchParams.has('login_challenge'));
  return { pkce, state };
}
