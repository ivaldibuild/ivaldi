import { signal } from '@angular/core';
import { ApiError } from '../api/api-client';

/** Loading, saving and error state for a page that loads and saves through the API. */
export class RequestStatus {
  readonly loading = signal(true);
  readonly saving = signal(false);
  readonly error = signal<string | null>(null);

  /** Runs `work`, recording a failure's message instead of throwing. Returns whether it worked. */
  async run(work: () => Promise<unknown>, flag: 'loading' | 'saving' = 'saving'): Promise<boolean> {
    this[flag].set(true);
    this.error.set(null);
    try {
      await work();
      return true;
    } catch (e) {
      this.error.set(e instanceof ApiError ? e.message : 'Something went wrong. Try again.');
      return false;
    } finally {
      this[flag].set(false);
    }
  }
}
