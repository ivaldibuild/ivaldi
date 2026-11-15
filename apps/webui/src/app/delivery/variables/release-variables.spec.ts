import { ComponentFixture, TestBed } from '@angular/core/testing';
import { ReleaseVariables as ReleaseVariablesView } from '../../api/models';
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
        { value: 'info', scope },
        { value: 'warn', scope },
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

  async function render(
    view: ReleaseVariablesView,
    targetId: string | null = null,
  ): Promise<HTMLElement> {
    asked = [];
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
          },
        },
      ],
    });
    const fixture: ComponentFixture<ReleaseVariables> = TestBed.createComponent(ReleaseVariables);
    fixture.componentRef.setInput('releaseId', 'rel_1');
    fixture.componentRef.setInput('targetId', targetId);
    fixture.detectChanges();
    for (let i = 0; i < 3; i++) {
      await new Promise((resolve) => setTimeout(resolve));
      fixture.detectChanges();
    }
    return fixture.nativeElement as HTMLElement;
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
            winner: { value: 'warn', scope, source: 'application' },
            beaten: [{ value: 'info', scope, source: 'application' }],
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

  it('says when a Release has no snapshot', async () => {
    const el = await render({ revision: null, resolved: [] });
    expect(el.textContent).toContain('deploys with none');
  });
});
