import { computed, DOCUMENT, inject, Service, signal } from '@angular/core';
import { Workspace } from '../api/models';
import { DeliveryApi } from './delivery-api';

const storageKey = 'ivaldi.workspace';

/** The Workspace the delivery pages show, chosen in the sidebar and remembered per browser. */
@Service()
export class WorkspaceContext {
  private readonly api = inject(DeliveryApi);
  private readonly document = inject(DOCUMENT);

  readonly workspaces = signal<Workspace[]>([]);
  private readonly selectedId = signal<string | null>(this.remembered());
  private loading: Promise<void> | null = null;

  readonly current = computed(() => {
    const all = this.workspaces();
    return (
      all.find((w) => w.id === this.selectedId()) ?? all.find((w) => w.is_default) ?? all[0] ?? null
    );
  });

  /** Loads the Workspaces once; later calls reuse the first load. */
  load(): Promise<void> {
    this.loading ??= this.api.workspaces().then((all) => this.workspaces.set(all));
    return this.loading;
  }

  select(id: string): void {
    this.selectedId.set(id);
    try {
      this.document.defaultView?.localStorage.setItem(storageKey, id);
    } catch {
      // Remembering the choice is a convenience; the page works without it.
    }
  }

  private remembered(): string | null {
    try {
      return this.document.defaultView?.localStorage.getItem(storageKey) ?? null;
    } catch {
      return null;
    }
  }
}
