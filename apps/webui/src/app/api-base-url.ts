import { DOCUMENT, inject, InjectionToken } from '@angular/core';

/**
 * The control plane's address, such as `https://ivaldi.example.com`. Empty when the web
 * app is served from the control plane's own origin. Set it at deploy time with
 * `<meta name="ivaldi-api-url" content="…">` in index.html.
 */
export const API_BASE_URL = new InjectionToken<string>('API_BASE_URL', {
  providedIn: 'root',
  factory: () => {
    const meta = inject(DOCUMENT).querySelector('meta[name="ivaldi-api-url"]');
    return (meta?.getAttribute('content') ?? '').trim().replace(/\/+$/, '');
  },
});
