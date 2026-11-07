import { Component, computed, signal } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { provideRouter, Router } from '@angular/router';
import { CurrentUser } from '../api/current-user';
import { Session } from '../auth/session';
import { AppShell } from './app-shell';
import { WorkspaceContext } from './workspace-context';

@Component({ template: '' })
class Blank {}

describe('AppShell', () => {
  async function renderAt(url: string): Promise<HTMLElement> {
    const workspace = { id: 'wsp_1', slug: 'platform', display_name: 'Platform', is_default: true };
    TestBed.configureTestingModule({
      imports: [AppShell],
      providers: [
        provideRouter([{ path: '**', component: Blank }]),
        {
          provide: WorkspaceContext,
          useValue: {
            current: signal(workspace),
            workspaces: signal([workspace]),
            load: () => Promise.resolve(),
            select: () => undefined,
          },
        },
        { provide: Session, useValue: { signOut: () => undefined } },
        {
          provide: CurrentUser,
          useValue: {
            profile: signal(null),
            displayName: signal('Ama Mensah'),
            firstName: signal('Ama'),
            email: signal(null),
            organization: signal(null),
            role: signal('owner'),
            roleLabel: signal('Owner'),
            canManageOrganization: computed(() => true),
            initials: signal('AM'),
            reload: () => undefined,
          },
        },
      ],
    });
    const fixture = TestBed.createComponent(AppShell);
    await TestBed.inject(Router).navigateByUrl(url);
    fixture.detectChanges();
    await fixture.whenStable();
    fixture.detectChanges();
    return fixture.nativeElement as HTMLElement;
  }

  const delivery = (el: HTMLElement) =>
    [...el.querySelectorAll('nav[aria-label="Primary"] a')]
      .map((a) => a.textContent?.trim())
      .filter((label) =>
        ['Projects', 'Environments', 'Lifecycles', 'Applications'].includes(label!),
      );

  it('reaches Applications through Projects rather than a menu of their own', async () => {
    const el = await renderAt('/projects');
    expect(delivery(el)).toEqual(['Projects', 'Environments', 'Lifecycles']);
  });

  it("marks Projects as current on an Application's and a Release's pages", async () => {
    for (const url of ['/applications/app_1', '/releases/rel_1', '/projects/prj_1/edit']) {
      TestBed.resetTestingModule();
      const el = await renderAt(url);
      const current = el.querySelector('nav[aria-label="Primary"] a[aria-current="page"]');
      expect(current?.textContent?.trim(), url).toBe('Projects');
    }
  });

  it('marks only the current page', async () => {
    const el = await renderAt('/people');
    const current = [...el.querySelectorAll('nav[aria-label="Primary"] a[aria-current="page"]')];
    expect(current.map((a) => a.textContent?.trim())).toEqual(['People & access']);
  });
});
