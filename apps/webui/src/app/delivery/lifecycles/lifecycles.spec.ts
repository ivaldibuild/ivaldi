import { signal, Type } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideRouter, Router } from '@angular/router';
import { ApiError } from '../../api/api-client';
import { Lifecycle } from '../../api/models';
import { PermissionsApi } from '../../api/permissions-api';
import { DeliveryApi } from '../delivery-api';
import { WorkspaceContext } from '../workspace-context';
import { LifecyclePage } from './lifecycle-page';
import { LifecyclesApi, PhaseFields } from './lifecycles-api';
import { LifecyclesPage } from './lifecycles-page';
import { NewLifecyclePage } from './new-lifecycle-page';

const environments = [
  { id: 'env_stg', display_name: 'Staging', deleted_at: null },
  { id: 'env_eu', display_name: 'Production EU', deleted_at: null },
  { id: 'env_us', display_name: 'Production US', deleted_at: null },
];

const standard = (): Lifecycle =>
  ({
    id: 'lc_std',
    workspace_id: 'wsp_1',
    display_name: 'Standard',
    description: '',
    deleted_at: null,
    phases: [
      { id: 'ph_stg', name: 'Staging', environment_ids: ['env_stg'], position: 0 },
      { id: 'ph_prd', name: 'Production', environment_ids: ['env_eu', 'env_us'], position: 1 },
    ],
  }) as unknown as Lifecycle;

describe('Lifecycle screens', () => {
  /** What the pages asked the control plane to do, as [operation, ...arguments]. */
  let calls: unknown[][];
  let archive: () => Promise<Lifecycle>;

  async function render<T>(
    component: Type<T>,
    granted: string[],
    inputs: Record<string, unknown> = {},
  ) {
    calls = [];
    archive = () => Promise.resolve(standard());
    const record =
      (name: string) =>
      (...args: unknown[]) => {
        calls.push([name, ...args]);
        return Promise.resolve({ ...standard(), id: name === 'create' ? 'lc_new' : 'lc_std' });
      };
    TestBed.configureTestingModule({
      imports: [component],
      providers: [
        provideRouter([]),
        {
          provide: LifecyclesApi,
          useValue: {
            list: () => Promise.resolve([standard()]),
            get: () => Promise.resolve(standard()),
            workspaceDefault: () => Promise.resolve(null),
            create: record('create'),
            updatePhase: record('updatePhase'),
            archive: () => archive(),
          },
        },
        { provide: DeliveryApi, useValue: { environments: () => Promise.resolve(environments) } },
        { provide: PermissionsApi, useValue: { granted: () => Promise.resolve(new Set(granted)) } },
        {
          provide: WorkspaceContext,
          useValue: { current: signal({ id: 'wsp_1', display_name: 'Platform' }) },
        },
      ],
    });
    vi.spyOn(TestBed.inject(Router), 'navigate').mockResolvedValue(true);
    const fixture = TestBed.createComponent(component);
    for (const [name, value] of Object.entries(inputs)) {
      fixture.componentRef.setInput(name, value);
    }
    fixture.detectChanges();
    await settle(fixture);
    return { fixture, el: fixture.nativeElement as HTMLElement };
  }

  async function settle(fixture: ComponentFixture<unknown>): Promise<void> {
    for (let i = 0; i < 4; i++) {
      await new Promise((resolve) => setTimeout(resolve));
      fixture.detectChanges();
    }
  }

  const button = (el: HTMLElement, label: string) =>
    [...el.querySelectorAll('button')].find(
      (b) => b.getAttribute('aria-label') === label || b.textContent?.trim() === label,
    );

  function type(el: HTMLElement, selector: string, value: string): void {
    const input = el.querySelector<HTMLInputElement>(selector)!;
    input.value = value;
    input.dispatchEvent(new Event('input'));
  }

  /** Ticks the Environment checkbox labelled `name` inside `within`. */
  function tick(within: Element, name: string): void {
    const label = [...within.querySelectorAll('label')].find((l) => l.textContent?.includes(name));
    label!.querySelector('input')!.click();
  }

  it('creates a Lifecycle with its phases in order', async () => {
    const { fixture, el } = await render(NewLifecyclePage, ['lifecycle.manage']);
    type(el, '#lifecycle-name', 'Regulated');
    type(el, '#phase-0-name', 'Staging');
    fixture.detectChanges();

    el.querySelector('form')!.dispatchEvent(new Event('submit'));
    await settle(fixture);
    expect(el.querySelector('[role="alert"]')?.textContent).toContain(
      'Choose at least one Environment for Staging.',
    );
    expect(calls).toEqual([]);

    const phases = () => el.querySelectorAll('section ol > li');
    tick(phases()[0], 'Staging');
    button(el, 'Add phase')!.click();
    fixture.detectChanges();
    type(el, '#phase-1-name', 'Production');
    tick(phases()[1], 'Production EU');
    tick(phases()[1], 'Production US');
    fixture.detectChanges();
    expect(phases()[1].textContent).toContain('Staging (in Staging)');
    const count = el.querySelector<HTMLSelectElement>('#phase-1-count')!;
    count.value = '1';
    count.dispatchEvent(new Event('change'));
    el.querySelector('form')!.dispatchEvent(new Event('submit'));
    await settle(fixture);

    expect(calls).toEqual([
      [
        'create',
        'wsp_1',
        {
          display_name: 'Regulated',
          description: '',
          phases: [
            { name: 'Staging', environment_ids: ['env_stg'], optional: false },
            {
              name: 'Production',
              environment_ids: ['env_eu', 'env_us'],
              optional: false,
              min_environments: 1,
            },
          ],
        } satisfies { phases: PhaseFields[] } & Record<string, unknown>,
      ],
    ]);
    expect(TestBed.inject(Router).navigate).toHaveBeenCalledWith(['/lifecycles', 'lc_new']);
  });

  it('saves an edited phase', async () => {
    const { fixture, el } = await render(LifecyclePage, ['lifecycle.manage'], { id: 'lc_std' });
    button(el, 'Edit Production')!.click();
    fixture.detectChanges();
    const editor = el.querySelector('form')!;
    expect(editor.textContent).toContain('Staging (in Staging)');
    type(el, '#phase-editor-name', 'Production (EU only)');
    tick(editor, 'Production US');
    editor.dispatchEvent(new Event('submit'));
    await settle(fixture);

    expect(calls).toEqual([
      [
        'updatePhase',
        'lc_std',
        'ph_prd',
        { name: 'Production (EU only)', environment_ids: ['env_eu'], optional: false },
      ],
    ]);
  });

  it("says why a Lifecycle that Channels follow can't be archived", async () => {
    const { fixture, el } = await render(LifecyclePage, ['lifecycle.manage'], { id: 'lc_std' });
    archive = () => Promise.reject(new ApiError(409, '2 active channels follow lifecycle lc_std.'));
    button(el, 'Archive')!.click();
    fixture.detectChanges();
    button(el, 'Archive Lifecycle')!.click();
    await settle(fixture);

    expect(el.textContent).toContain("It wasn't archived");
    expect(el.textContent).toContain('2 active channels follow lifecycle lc_std.');
  });

  it('offers no changes to someone who may not manage Lifecycles', async () => {
    const detail = await render(LifecyclePage, [], { id: 'lc_std' });
    for (const label of ['Rename', 'Make default', 'Archive', 'Add phase', 'Edit Production']) {
      expect(button(detail.el, label), label).toBeUndefined();
    }
    TestBed.resetTestingModule();

    const list = await render(LifecyclesPage, []);
    expect(list.el.querySelector('a[href="/lifecycles/new"]')).toBeNull();
    expect(list.el.querySelector('a[href="/lifecycles/lc_std"]')).not.toBeNull();
  });
});
