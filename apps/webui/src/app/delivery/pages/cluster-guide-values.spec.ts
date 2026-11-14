import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import Ajv from 'ajv';
import { parse } from 'yaml';
import { AgentChoices, agentValues } from './cluster-guide-values';

/** The agent chart's schema, read from the repository. */
const schema = JSON.parse(
  readFileSync(resolve(process.cwd(), '../../deploy/helm/ivaldi-agent/values.schema.json'), 'utf8'),
);
const validate = new Ajv({ allErrors: true, strict: false }).compile(schema);

/** Every combination of the step 2 choices that changes the values. */
function combinations(): AgentChoices[] {
  const out: AgentChoices[] = [];
  for (const observeAll of [true, false]) {
    for (const executeNamespaces of [
      '',
      'payments-prod',
      'payments-staging, payments-prod payments-prod',
    ]) {
      for (const allowRBAC of [false, true]) {
        for (const authentication of ['bearer', 'kubernetes'] as const) {
          out.push({
            observeAll,
            observeNamespaces: observeAll ? '' : 'reporting, payments-prod',
            executeNamespaces,
            allowRBAC,
            agentNamespace: 'ivaldi-system',
            authentication,
          });
        }
      }
    }
  }
  return out;
}

describe('the guide writes valid ivaldi-agent values', () => {
  for (const choices of combinations()) {
    it(`for ${JSON.stringify(choices)}`, () => {
      const values = parse(agentValues('https://ivaldi.example.com', choices));
      const valid = validate(values);
      expect(validate.errors ?? []).toEqual([]);
      expect(valid).toBe(true);
    });
  }

  it('allows a plaintext control plane only when it is one', () => {
    const local = parse(agentValues('http://localhost:8660', combinations()[0]));
    expect(local.controlPlane.allowInsecureHTTP).toBe(true);
    expect(validate(local)).toBe(true);
    const remote = parse(agentValues('https://ivaldi.example.com', combinations()[0]));
    expect(remote.controlPlane.allowInsecureHTTP).toBeUndefined();
    expect(validate({ controlPlane: { url: 'http://ivaldi.example.com' } })).toBe(false);
  });

  it('never writes the enrolment token', () => {
    const values = parse(agentValues('https://ivaldi.example.com', combinations()[0]));
    expect(values.enrolment).toEqual({ secretName: 'ivaldi-enrolment' });
  });
});
