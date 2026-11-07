import { DOCUMENT, inject, Service, signal } from '@angular/core';

const storageKey = 'ivaldi-theme';

/** Forge, the dark theme and the default, or Paper, the light one; remembered in this browser. */
@Service()
export class Theme {
  private readonly document = inject(DOCUMENT);
  readonly dark = signal(this.remembered() !== 'light');

  constructor() {
    this.apply();
  }

  toggle(): void {
    this.dark.update((dark) => !dark);
    this.apply();
    try {
      this.document.defaultView?.localStorage.setItem(storageKey, this.dark() ? 'dark' : 'light');
    } catch {
      // Storage can be unavailable, such as in a private window; the choice lasts this visit.
    }
  }

  private apply(): void {
    this.document.documentElement.classList.toggle('dark', this.dark());
  }

  private remembered(): string | null {
    try {
      return this.document.defaultView?.localStorage.getItem(storageKey) ?? null;
    } catch {
      return null;
    }
  }
}
