import { DOCUMENT, inject, InjectionToken } from '@angular/core';

/**
 * The control plane's address, such as `https://ivaldi.example.com`. Empty when the web
 * app is served from the control plane's own origin. It comes from
 * `<meta name="ivaldi-api-url" content="…">` in index.html, which the web app's image
 * fills from `IVALDI_API_URL` when it starts (see docs/self-hosting/web-app.md).
 */
export const API_BASE_URL = new InjectionToken<string>('API_BASE_URL', {
  providedIn: 'root',
  factory: () => {
    const meta = inject(DOCUMENT).querySelector('meta[name="ivaldi-api-url"]');
    return (meta?.getAttribute('content') ?? '').trim().replace(/\/+$/, '');
  },
});
