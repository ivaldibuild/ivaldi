import { ComponentFixture, TestBed } from '@angular/core/testing';
import {
  ReleaseVariables as ReleaseVariablesView,
  ReleaseVariablesRefresh,
} from '../../api/models';
import { ReleaseVariables } from './release-variables';
import { VariablesApi } from './variables-api';

const scope = { environment_ids: [], target_ids: [], channel_ids: [] };
const revision = {
  id: 'vsn_1',
  organization_id: 'org_1',
  application_id: 'app_1',
  release_id: 'rel_1',
  revision: 2,
  status: 'current',
  variables: [
    {
      name: 'Log.Level',
      source: 'application',
      kind: 'text',
      values_path: 'log.level',
      deliver_as: null,
      values: [
        { value: 'info', secret: null, scope },
        { value: 'warn', secret: null, scope },
      ],
    },
    {
      name: 'Region',
      source: 'project',
      kind: 'text',
      values_path: null,
      deliver_as: null,
      values: [],
    },
  ],
  taken_by_principal_id: 'prn_1',
  created_at: '2026-11-15T10:00:00Z',
  updated_at: '2026-11-15T10:00:00Z',
} as NonNullable<ReleaseVariablesView['revision']>;

describe('ReleaseVariables', () => {
  let asked: (string | undefined)[];
  let refreshed: boolean[];
  let refresh: ReleaseVariablesRefresh;
  let fixture: ComponentFixture<ReleaseVariables>;

  async function render(
    view: ReleaseVariablesView,
    targetId: string | null = null,
    refreshable = false,
  ): Promise<HTMLElement> {
    asked = [];
    refreshed = [];
    TestBed.configureTestingModule({
      imports: [ReleaseVariables],
      providers: [
        {
          provide: VariablesApi,
          useValue: {
            releaseVariables: (_releaseId: string, target?: string) => {
              asked.push(target);
              return Promise.resolve(view);
            },
            refreshReleaseVariables: (_releaseId: string, preview: boolean) => {
              refreshed.push(preview);
              return Promise.resolve(refresh);
            },
          },
        },
      ],
    });
    fixture = TestBed.createComponent(ReleaseVariables);
    fixture.componentRef.setInput('releaseId', 'rel_1');
    fixture.componentRef.setInput('targetId', targetId);
    fixture.componentRef.setInput('refreshable', refreshable);
    fixture.detectChanges();
    await settle();
    return fixture.nativeElement as HTMLElement;
  }

  async function settle(): Promise<void> {
    for (let i = 0; i < 4; i++) {
      await new Promise((resolve) => setTimeout(resolve));
      fixture.detectChanges();
    }
  }

  function button(el: HTMLElement, label: string): HTMLButtonElement {
    const found = [...el.querySelectorAll('button')].find((b) => b.textContent?.trim() === label);
    expect(found, `a button labelled "${label}"`).toBeTruthy();
    return found!;
  }

  const rows = (el: HTMLElement) =>
    [...el.querySelectorAll('tbody tr')].map((row) =>
      [...row.querySelectorAll('td')].map((cell) => cell.textContent?.replace(/\s+/g, ' ').trim()),
    );

  it("shows the snapshot's revision and what it holds", async () => {
    const el = await render({ revision, resolved: [] });

    expect(asked).toEqual([undefined]);
    expect(el.textContent).toContain('Revision 2');
    expect(rows(el)).toEqual([
      ['Log.Level', 'log.level', '2', 'Application'],
      ['Region', 'Bindings only', '0', 'Project'],
    ]);
  });

  it('shows the value a Target gets, and a variable it does not', async () => {
    const el = await render(
      {
        revision,
        resolved: [
          {
            name: 'Log.Level',
            kind: 'text',
            values_path: 'log.level',
            deliver_as: null,
            winner: { value: 'warn', secret: null, scope, source: 'application' },
            beaten: [{ value: 'info', secret: null, scope, source: 'application' }],
          },
          {
            name: 'Region',
            kind: 'text',
            values_path: null,
            deliver_as: null,
            winner: null,
            beaten: [],
          },
        ],
      },
      'tgt_1',
    );

    expect(asked).toEqual(['tgt_1']);
    expect(rows(el)).toEqual([
      ['Log.Level Written to log.level', 'warn', 'Application Beat 1 less specific value'],
      ['Region', "No value applies here, so this Target doesn't get it"],
    ]);
  });

  it('previews an update, and takes the revision when production is not changed', async () => {
    const el = await render({ revision, resolved: [] }, null, true);
    refresh = { revision, applied: false, changed: ['Log.Level'], production: [] };

    button(el, 'Update variables').click();
    await settle();
    expect(el.textContent).toContain('Updating would change Log.Level');
    button(el, 'Take new revision').click();
    await settle();
    expect(refreshed).toEqual([true, false]);
    expect(asked.length, 'the snapshot is read again').toBe(2);
  });

  it('does not offer an update that changes production', async () => {
    const el = await render({ revision, resolved: [] }, null, true);
    refresh = {
      revision,
      applied: false,
      changed: ['Log.Level'],
      production: [{ target_id: 'tgt_1', environment_id: 'env_1', variables: ['Log.Level'] }],
    };

    button(el, 'Update variables').click();
    await settle();
    expect(el.textContent).toContain('on 1 production Target');
    expect([...el.querySelectorAll('button')].map((b) => b.textContent?.trim())).toEqual(['Close']);
  });

  it('says when a Release has no snapshot', async () => {
    const el = await render({ revision: null, resolved: [] });
    expect(el.textContent).toContain('deploys with none');
  });
});
