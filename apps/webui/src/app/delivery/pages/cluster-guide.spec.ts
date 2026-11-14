import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { Cluster } from '../../api/models';
import { API_BASE_URL } from '../../api-base-url';
import { ClusterGuide } from './cluster-guide';

const v1 = 'https://cp.test/v1';
const ok = <T>(data: T) => ({ status: 'success', data });

const enrolled = {
  id: 'cls_1',
  slug: 'prod-eu',
  display_name: 'Prod EU',
  agent_principal_id: 'prn_old',
} as unknown as Cluster;

function issued(chartVersion = '1.4.0') {
  return ok({
    cluster_id: 'cls_1',
    token: 'iwe_secret',
    method: 'bearer',
    pool: 'cluster:cls_1',
    expires_at: '2099-01-01T00:00:00Z',
    install: {
      server_url: 'https://cp.test',
      chart: 'oci://ghcr.io/ivaldibuild/charts/ivaldi-agent',
      chart_version: chartVersion,
      credential_kinds: ['bearer'],
    },
  });
}

/** The status of an agent that has enrolled, heartbeated, and reported. */
function reporting(principal: string) {
  return ok({
    cluster_id: 'cls_1',
    status: 'active',
    agent_principal_id: principal,
    redeemed_at: '2026-11-14T12:00:00Z',
    last_seen_at: '2026-11-14T12:00:30Z',
    agent_version: '1.4.0',
    kubernetes_version: 'v1.31.4',
  });
}

describe('cluster guide', () => {
  let http: HttpTestingController;

  function render(
    cluster: Cluster | null,
    startAt: number,
  ): { fixture: ComponentFixture<ClusterGuide>; el: HTMLElement } {
    TestBed.configureTestingModule({
      imports: [ClusterGuide],
      providers: [
        provideHttpClient(),
        provideHttpClientTesting(),
        provideRouter([]),
        { provide: API_BASE_URL, useValue: 'https://cp.test' },
      ],
    });
    http = TestBed.inject(HttpTestingController);
    const fixture = TestBed.createComponent(ClusterGuide);
    fixture.componentRef.setInput('cluster', cluster);
    fixture.componentRef.setInput('startAt', startAt);
    fixture.detectChanges();
    return { fixture, el: fixture.nativeElement as HTMLElement };
  }

  async function settle(fixture: ComponentFixture<unknown>): Promise<void> {
    for (let i = 0; i < 3; i++) {
      await new Promise((resolve) => setTimeout(resolve));
    }
    await fixture.whenStable();
    fixture.detectChanges();
  }

  function click(el: HTMLElement, text: string): void {
    const button = [...el.querySelectorAll('button')].find((b) => b.textContent?.trim() === text);
    expect(button, `a "${text}" button`).toBeDefined();
    button!.click();
  }

  function button(el: HTMLElement, text: string): HTMLButtonElement | undefined {
    return [...el.querySelectorAll('button')].find((b) => b.textContent?.trim() === text);
  }

  afterEach(() => {
    TestBed.resetTestingModule();
    http.verify();
  });

  it('re-enrolling mints a token and waits for a new agent, not the old one', async () => {
    const { fixture, el } = render(enrolled, 3);
    http
      .expectOne((r) => r.method === 'POST' && r.url === `${v1}/clusters/cls_1/enrolments`)
      .flush(issued());
    await settle(fixture);
    expect(el.textContent).toContain('iwe_secret');

    click(el, "I've installed it");
    await settle(fixture);
    http.expectOne(`${v1}/clusters/cls_1/agent`).flush(reporting('prn_old'));
    await settle(fixture);
    expect(el.textContent, 'the old agent does not count').not.toContain('The agent is in contact');
    expect(el.querySelectorAll('ng-icon[name="lucideCheck"]').length).toBe(0);
    expect(el.textContent, 'the token is gone once the install step is left').not.toContain(
      'iwe_secret',
    );

    fixture.componentInstance['refresh']();
    http.expectOne(`${v1}/clusters/cls_1/agent`).flush(reporting('prn_new'));
    await settle(fixture);
    expect(el.textContent).toContain('The agent is in contact');
    expect(el.querySelectorAll('ng-icon[name="lucideCheck"]').length).toBe(4);
  });

  it('leaves the chart version out when the control plane is not a release build', async () => {
    const { fixture, el } = render(enrolled, 3);
    http
      .expectOne((r) => r.method === 'POST' && r.url === `${v1}/clusters/cls_1/enrolments`)
      .flush(issued(''));
    await settle(fixture);

    const commands = el.textContent ?? '';
    expect(commands).toContain(
      'helm upgrade --install ivaldi-agent oci://ghcr.io/ivaldibuild/charts/ivaldi-agent --namespace',
    );
    expect(commands).not.toContain('--version');
    expect(commands).toContain('read -rs IVALDI_ENROLMENT_TOKEN');
    expect(commands, 'the token never goes on a command line').not.toContain(
      '--from-literal=token=iwe_secret',
    );
  });

  it('refuses namespaces that would corrupt the values file', async () => {
    const { fixture, el } = render(
      { ...enrolled, agent_principal_id: null } as unknown as Cluster,
      2,
    );
    await settle(fixture);

    const execute = el.querySelector<HTMLInputElement>('#guide-execute')!;
    execute.value = 'payments, oops:true';
    execute.dispatchEvent(new Event('input'));
    await settle(fixture);

    expect(el.textContent).toContain('oops:true is not a Kubernetes namespace');
    expect(button(el, 'Continue')?.disabled).toBe(true);
    http.expectNone(`${v1}/clusters/cls_1/enrolments`);
  });
});
