import { expect, test } from '@playwright/test';
import { adminCredentials, apiURL, clientId, redirectURI, startSignIn } from './support/stack';

test.describe('sign-in', () => {
  test('says there is nothing to continue without a sign-in', async ({ page }) => {
    await page.goto('/login');

    await expect(
      page.getByRole('heading', { name: 'Open the application you want to use' }),
    ).toBeVisible();
    await expect(page.getByRole('textbox', { name: 'Email' })).toHaveCount(0);
  });

  test('asks the person to start again when the challenge is unknown', async ({ page }) => {
    await page.goto('/login?login_challenge=not-a-real-challenge');

    await expect(page.getByRole('heading', { name: 'This sign-in has timed out' })).toBeVisible();
  });

  test('offers the ways to sign in that the installation allows', async ({ page, request }) => {
    await startSignIn(page);
    const challenge = new URL(page.url()).searchParams.get('login_challenge');
    const response = await request.get(`${apiURL}/v1/auth/login`, {
      params: { login_challenge: challenge ?? '' },
    });
    expect(response.ok()).toBe(true);
    const offered = (await response.json()) as {
      password: boolean;
      providers: { name: string; start_url: string }[];
    };

    await expect(page.getByRole('heading', { name: 'Sign in' })).toBeVisible();
    await expect(page.getByText('to continue to Ivaldi.')).toBeVisible();
    for (const provider of offered.providers) {
      await expect(page.getByRole('link', { name: `Continue with ${provider.name}` })).toHaveAttribute(
        'href',
        provider.start_url,
      );
    }
    await expect(page.getByRole('textbox', { name: 'Email' })).toBeVisible({
      visible: offered.password,
    });
  });

  test('checks the form before sending it', async ({ page }) => {
    await startSignIn(page);

    await page.getByRole('button', { name: 'Sign in' }).click();

    await expect(page.getByText('Enter your email.')).toBeVisible();
    await expect(page.getByText('Enter your password.')).toBeVisible();

    await page.getByRole('textbox', { name: 'Email' }).fill('not-an-email');
    await page.getByRole('button', { name: 'Sign in' }).click();
    await expect(page.getByText('Enter an email address, like you@example.com.')).toBeVisible();
  });

  test('refuses a wrong password without saying which part was wrong', async ({ page }) => {
    await startSignIn(page);

    await page.getByRole('textbox', { name: 'Email' }).fill('nobody@ivaldi.local');
    await page.getByLabel('Password').fill('not the password');
    await page.getByRole('button', { name: 'Sign in' }).click();

    const alert = page.getByRole('alert');
    await expect(alert).toContainText("That didn't work");
    await expect(alert).toContainText('Check your email and password and try again.');
    await expect(page.getByLabel('Password')).toHaveValue('');
  });

  test('signs the administrator in and returns a code the web app can exchange', async ({
    page,
    request,
  }) => {
    const admin = adminCredentials();
    test.skip(
      !admin,
      'No admin credentials: set IVALDI_E2E_ADMIN_EMAIL and IVALDI_E2E_ADMIN_PASSWORD, or run make local_stack',
    );

    const { pkce, state } = await startSignIn(page);
    await page.getByRole('textbox', { name: 'Email' }).fill(admin!.email);
    await page.getByLabel('Password').fill(admin!.password);

    // The web app has no /auth/callback route yet and routes the address away as soon as
    // it loads, so the test keeps the request the control plane redirected the browser to.
    const callbackRequest = page.waitForRequest((r) => r.url().startsWith(`${redirectURI}?`));
    await page.getByRole('button', { name: 'Sign in' }).click();
    const callback = new URL((await callbackRequest).url());
    expect(callback.searchParams.get('state')).toBe(state);
    expect(callback.searchParams.get('iss')).toBe(apiURL);
    const code = callback.searchParams.get('code');
    expect(code).toBeTruthy();

    const token = await request.post(`${apiURL}/oauth/token`, {
      form: {
        grant_type: 'authorization_code',
        client_id: clientId,
        code: code!,
        redirect_uri: redirectURI,
        code_verifier: pkce.verifier,
      },
    });
    expect(token.status()).toBe(200);
    const body = (await token.json()) as Record<string, unknown>;
    expect(body['token_type']).toMatch(/^bearer$/i);
    expect(body['access_token']).toEqual(expect.any(String));
    expect(body['refresh_token']).toEqual(expect.any(String));
  });
});
