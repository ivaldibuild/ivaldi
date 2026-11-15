import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { ApiError } from '../../api/api-client';
import { Variable, VariableScope } from '../../api/models';
import { PermissionsApi } from '../../api/permissions-api';
import { VariableList } from './variable-list';
import { ValueFields, VariableFields, VariablesApi } from './variables-api';

const production = 'env_01JZ0000000000000000000001';
const staging = 'env_01JZ0000000000000000000002';
const scope = (changes: Partial<VariableScope> = {}): VariableScope => ({
  environment_ids: [],
  target_ids: [],
  channel_ids: [],
  ...changes,
});

function variable(
  id: string,
  name: string,
  owner: { project_id: string } | { application_id: string },
  values: { value: string; scope: VariableScope }[] = [],
  values_path: string | null = null,
): Variable {
  const at = '2026-11-15T10:00:00Z';
  return {
    id,
    organization_id: 'org_1',
    project_id: null,
    application_id: null,
    ...owner,
    name,
    description: null,
    kind: 'text',
    values_path,
    deliver_as: null,
    values: values.map((v, i) => ({ id: `${id}-v${i}`, created_at: at, updated_at: at, ...v })),
    created_at: at,
    updated_at: at,
    archived_at: null,
  };
}

const stored = (): Variable[] => [
  variable(
    'var_log',
    'Log.Level',
    { application_id: 'app_1' },
    [
      { value: 'info', scope: scope() },
      { value: 'warn', scope: scope({ environment_ids: [production] }) },
    ],
    'log.level',
  ),
  variable('var_host', 'database.host', { application_id: 'app_1' }),
  variable('var_shared_host', 'Database.Host', { project_id: 'prj_1' }),
  variable('var_region', 'Region', { project_id: 'prj_1' }, [], 'region'),
];

describe('VariableList', () => {
  let variables: Variable[];
  let added: { id: string; value: ValueFields }[];
  let created: VariableFields[];
  let refuse: ApiError | null;
  let forbidScopes = false;

  afterEach(() => (forbidScopes = false));

  async function render(
    granted = ['variable.manage'],
  ): Promise<{ fixture: ComponentFixture<VariableList>; el: HTMLElement }> {
    variables = stored();
    added = [];
    created = [];
    refuse = null;
    const api: Partial<VariablesApi> = {
      list: () => Promise.resolve(variables),
      scopeOptions: () =>
        forbidScopes
          ? Promise.reject(new ApiError(403, "You don't have permission to do this."))
          : Promise.resolve({
              environments: [
                { id: production, display_name: 'Production' },
                { id: staging, display_name: 'Staging' },
              ],
              targets: [],
            } as unknown as Awaited<ReturnType<VariablesApi['scopeOptions']>>),
      addValue: (id, value) => {
        if (refuse) {
          return Promise.reject(refuse);
        }
        added.push({ id, value });
        return Promise.resolve(variables[0]);
      },
      create: (_owner, fields) => {
        created.push(fields);
        return Promise.resolve(variables[0]);
      },
    };
    TestBed.configureTestingModule({
      imports: [VariableList],
      providers: [
        provideRouter([]),
        { provide: VariablesApi, useValue: api },
        { provide: PermissionsApi, useValue: { granted: () => Promise.resolve(new Set(granted)) } },
      ],
    });
    const fixture = TestBed.createComponent(VariableList);
    fixture.componentRef.setInput('projectId', 'prj_1');
    fixture.componentRef.setInput('applicationId', 'app_1');
    fixture.componentRef.setInput('workspaceId', 'wsp_1');
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

  function button(el: HTMLElement, label: string): HTMLButtonElement {
    const found = [...el.querySelectorAll('button')].find(
      (b) => b.getAttribute('aria-label') === label || b.textContent?.trim() === label,
    );
    expect(found, `a button labelled "${label}"`).toBeTruthy();
    return found!;
  }

  it('shows what each value applies to, and what the Project gives the Application', async () => {
    const { el } = await render();

    const rows = [...el.querySelectorAll('tbody tr')].map((row) =>
      [...row.querySelectorAll('td')].slice(0, 2).map((cell) => cell.textContent?.trim()),
    );
    expect(rows).toEqual([
      ['info', 'Every Deployment'],
      ['warn', 'Environment Production'],
    ]);

    const inherited = [...el.querySelectorAll('ul')].at(-1)!;
    const items = [...inherited.querySelectorAll('li')].map((li) => li.textContent);
    expect(items[0]).toContain('Database.Host');
    expect(items[0], 'the Application declares database.host').toContain('Overridden here');
    expect(items[1]).toContain('Region');
    expect(items[1]).not.toContain('Overridden here');
  });

  it('adds a value scoped to the ticked Environments', async () => {
    const { fixture, el } = await render();
    button(el, 'Add value to Log.Level').click();
    fixture.detectChanges();

    const form = el.querySelector<HTMLFormElement>('form[aria-label="Add a value to Log.Level"]')!;
    const text = form.querySelector('textarea')!;
    text.value = 'debug';
    text.dispatchEvent(new Event('input'));
    const staged = [...form.querySelectorAll('label')].find((l) =>
      l.textContent?.includes('Staging'),
    )!;
    staged.querySelector('input')!.click();
    form.dispatchEvent(new Event('submit'));
    await settle(fixture);

    expect(added).toEqual([
      { id: 'var_log', value: { value: 'debug', scope: scope({ environment_ids: [staging] }) } },
    ]);
    expect(el.querySelector('form'), 'the form closes once saved').toBeNull();
  });

  it('says why a value was refused, naming what it clashes with', async () => {
    const { fixture, el } = await render();
    button(el, 'Add value to Log.Level').click();
    fixture.detectChanges();
    refuse = new ApiError(
      422,
      `Variable Log.Level already has a value for environment ${production}.`,
    );
    el.querySelector('form')!.dispatchEvent(new Event('submit'));
    await settle(fixture);

    expect(el.querySelector('form [role="alert"]')?.textContent).toContain(
      'already has a value for environment Production.',
    );
  });

  it("starts an override from the Project's variable", async () => {
    const { fixture, el } = await render();
    button(el, 'Override Region for this Application').click();
    fixture.detectChanges();
    await settle(fixture);
    el.querySelector('form')!.dispatchEvent(new Event('submit'));
    await settle(fixture);

    expect(created).toEqual([
      { name: 'Region', values_path: 'region', description: undefined, values: [] },
    ]);
  });

  it('still lists variables for someone who may not read the Environments', async () => {
    forbidScopes = true;
    const { el } = await render();
    expect(el.querySelector('[role="alert"]')).toBeNull();
    const applies = [...el.querySelectorAll('tbody tr td:nth-child(2)')].map((cell) =>
      cell.textContent?.trim(),
    );
    expect(applies).toEqual(['Every Deployment', `Environment ${production}`]);
  });

  it('offers no changes without variable.manage on the Project', async () => {
    const { el } = await render([]);
    expect(el.textContent).toContain('Log.Level');
    expect(el.querySelectorAll('button').length).toBe(0);
  });
});
