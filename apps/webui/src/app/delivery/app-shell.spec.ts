import { Component, computed, signal } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { provideRouter, Router } from '@angular/router';
import { CurrentUser } from '../api/current-user';
import { Session } from '../auth/session';
import { AppShell } from './app-shell';
import { ProjectsApi } from './projects-api';
import { WorkspaceContext } from './workspace-context';

@Component({ template: '' })
class Blank {}

describe('AppShell', () => {
  let signOut: ReturnType<typeof vi.fn>;
  let select: ReturnType<typeof vi.fn>;
  let projectPages: ReturnType<typeof vi.fn>;

  async function renderAt(url: string): Promise<HTMLElement> {
    const workspace = { id: 'wsp_1', slug: 'platform', display_name: 'Platform', is_default: true };
    const data = { id: 'wsp_2', slug: 'data', display_name: 'Data', is_default: false };
    signOut = vi.fn();
    select = vi.fn();
    projectPages = vi.fn().mockResolvedValue({
      items: [
        { id: 'prj_1', display_name: 'Checkout' },
        { id: 'prj_2', display_name: 'Payments' },
      ],
      has_next: false,
    });
    TestBed.configureTestingModule({
      imports: [AppShell],
      providers: [
        provideRouter([{ path: '**', component: Blank }]),
        {
          provide: WorkspaceContext,
          useValue: {
            current: signal(workspace),
            workspaces: signal([workspace, data]),
            load: () => Promise.resolve(),
            select,
          },
        },
        { provide: Session, useValue: { signOut } },
        { provide: ProjectsApi, useValue: { page: projectPages } },
        {
          provide: CurrentUser,
          useValue: {
            profile: signal({}),
            displayName: signal('Ama Mensah'),
            firstName: signal('Ama'),
            email: signal('ama@northwind.dev'),
            organization: signal({ id: 'org_1', display_name: 'Northwind', logo_url: null }),
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
        ['Projects', 'Environments', 'Lifecycles', 'Applications', 'People & access'].includes(
          label!,
        ),
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
    const el = await renderAt('/environments');
    const current = [...el.querySelectorAll('nav[aria-label="Primary"] a[aria-current="page"]')];
    expect(current.map((a) => a.textContent?.trim())).toEqual(['Environments']);
  });

  it("lists the Workspace's Projects under Projects, marking the one showing", async () => {
    const el = await renderAt('/projects/prj_2');
    expect(projectPages).toHaveBeenCalledWith('wsp_1', {
      search: '',
      ownerTeamId: '',
      includeArchived: false,
    });
    const sub = el.querySelector('ul[aria-label="Projects in this Workspace"]')!;
    const links = [...sub.querySelectorAll('a')];
    expect(links.map((a) => a.textContent?.trim())).toEqual(['Checkout', 'Payments']);
    expect(links[1].getAttribute('href')).toBe('/projects/prj_2');
    expect(links[1].getAttribute('aria-current')).toBe('page');
    expect(links[0].getAttribute('aria-current')).toBeNull();
  });

  it('keeps Organisation settings out of the sidebar', async () => {
    const el = await renderAt('/projects');
    expect(delivery(el)).not.toContain('People & access');
  });

  it('folds the sidebar to icons and back from the trigger', async () => {
    const el = await renderAt('/projects');
    const sidebar = el.querySelector('hlm-sidebar')!;
    const trigger = el.querySelector<HTMLButtonElement>('button[data-sidebar="trigger"]')!;
    expect(trigger.textContent).toContain('Collapse or expand the sidebar');
    expect(sidebar.getAttribute('data-state')).toBe('expanded');

    trigger.click();
    TestBed.tick();
    expect(sidebar.getAttribute('data-state')).toBe('collapsed');
    expect(sidebar.getAttribute('data-collapsible')).toBe('icon');

    trigger.click();
    TestBed.tick();
    expect(sidebar.getAttribute('data-state')).toBe('expanded');
  });

  /** Opens a menu from its sidebar button and returns the menu, which renders in an overlay. */
  async function openMenu(el: HTMLElement, region: 'header' | 'footer'): Promise<HTMLElement> {
    el.querySelector<HTMLButtonElement>(
      `[data-sidebar="${region}"] button[data-sidebar="menu-button"]`,
    )!.click();
    await new Promise((resolve) => setTimeout(resolve));
    TestBed.tick();
    return document.querySelector<HTMLElement>('[data-slot="dropdown-menu"]')!;
  }

  const item = (menu: HTMLElement, text: string) =>
    [...menu.querySelectorAll<HTMLElement>('[data-slot="dropdown-menu-item"]')].find((i) =>
      i.textContent?.includes(text),
    );

  it("opens the person's menu from the footer, with their details and Sign out", async () => {
    const el = await renderAt('/projects');
    const footer = el.querySelector('[data-sidebar="footer"]')!;
    expect(footer.textContent).toContain('Ama Mensah');
    expect(footer.textContent).toContain('Owner');

    const menu = await openMenu(el, 'footer');
    expect(menu.textContent).toContain('ama@northwind.dev');
    expect(menu.textContent).toContain('Owner of Northwind');
    expect(item(menu, 'Organisation settings')?.getAttribute('href')).toBe('/settings');

    item(menu, 'Sign out')!.click();
    expect(signOut).toHaveBeenCalled();
  });

  it('switches between the light and dark themes from the same menu', async () => {
    const el = await renderAt('/projects');
    const html = document.documentElement;
    const wasDark = html.classList.contains('dark');

    const menu = await openMenu(el, 'footer');
    item(menu, wasDark ? 'Use the light theme' : 'Use the dark theme')!.click();
    expect(html.classList.contains('dark')).toBe(!wasDark);

    const again = await openMenu(el, 'footer');
    item(again, wasDark ? 'Use the dark theme' : 'Use the light theme')!.click();
    expect(html.classList.contains('dark')).toBe(wasDark);
  });

  it('switches Workspace from the header', async () => {
    const el = await renderAt('/projects');
    expect(el.querySelector('[data-sidebar="header"]')?.textContent).toContain(
      'Platform Workspace',
    );

    const menu = await openMenu(el, 'header');
    expect(item(menu, 'Platform')?.getAttribute('aria-current')).toBe('true');
    expect(item(menu, 'Create and manage Workspaces')?.getAttribute('href')).toBe(
      '/settings/workspaces',
    );
    item(menu, 'Data')!.click();
    expect(select).toHaveBeenCalledWith('wsp_2');
  });

  it("leaves a page of the old Workspace's for the list in the new one", async () => {
    const el = await renderAt('/projects/prj_1');
    const router = TestBed.inject(Router);

    const menu = await openMenu(el, 'header');
    item(menu, 'Data')!.click();
    await new Promise((resolve) => setTimeout(resolve));
    expect(select).toHaveBeenCalledWith('wsp_2');
    expect(router.url).toBe('/projects');
  });

  it('folds a group of the menu away and back', async () => {
    const el = await renderAt('/projects');
    const label = [
      ...el.querySelectorAll<HTMLButtonElement>('button[data-sidebar="group-label"]'),
    ].find((b) => b.textContent?.includes('Delivery'))!;
    expect(label.getAttribute('aria-expanded')).toBe('true');

    label.click();
    TestBed.tick();
    expect(label.getAttribute('aria-expanded')).toBe('false');

    label.click();
    TestBed.tick();
    expect(label.getAttribute('aria-expanded')).toBe('true');
  });
});
