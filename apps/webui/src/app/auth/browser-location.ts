import { DOCUMENT, inject, Service } from '@angular/core';

/** Full-page navigation, kept behind a service so tests can observe it. */
@Service()
export class BrowserLocation {
  private readonly document = inject(DOCUMENT);

  get host(): string {
    return this.document.location.host;
  }

  assign(url: string): void {
    this.document.location.assign(url);
  }
}
