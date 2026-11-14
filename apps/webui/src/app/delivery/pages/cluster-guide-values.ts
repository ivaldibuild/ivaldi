/**
 * The ivaldi-agent chart's values the cluster onboarding guide writes, from
 * the person's choices in step 2 and what the control plane reports. A test
 * checks every combination against the chart's values.schema.json, so a
 * chart change that breaks the guide fails CI.
 */

/** What the person chose in step 2. */
export interface AgentChoices {
  observeAll: boolean;
  observeNamespaces: string;
  executeNamespaces: string;
  allowRBAC: boolean;
  agentNamespace: string;
  authentication: 'bearer' | 'kubernetes';
}

/** The Secrets the guide's commands create and the chart reads. */
export const enrolmentSecretName = 'ivaldi-enrolment';

/** Splits a comma- or space-separated list of namespaces. */
export function namespaceList(value: string): string[] {
  return [
    ...new Set(
      value
        .split(/[\s,]+/)
        .map((v) => v.trim())
        .filter(Boolean),
    ),
  ];
}

/**
 * The values.yaml for the chart. It never holds the enrolment token: the
 * guide puts that in a Secret the person creates with kubectl.
 */
export function agentValues(serverURL: string, c: AgentChoices): string {
  const observe = c.observeAll ? [] : namespaceList(c.observeNamespaces);
  const execute = namespaceList(c.executeNamespaces);
  const lines = [
    'controlPlane:',
    `  url: ${serverURL}`,
    // The agent refuses a plaintext control plane unless told otherwise,
    // which only a local control plane should need.
    ...(serverURL.startsWith('http://') ? ['  allowInsecureHTTP: true'] : []),
    'agent:',
    `  authentication: ${c.authentication}`,
    'enrolment:',
    `  secretName: ${enrolmentSecretName}`,
    'state:',
    '  secretName: ivaldi-state',
    'fingerprint:',
    '  secretName: ivaldi-fingerprint-key',
    'observe:',
    namespaces('  ', observe),
    'execute:',
    namespaces('  ', execute),
    `  allowRBAC: ${c.allowRBAC}`,
  ];
  return `${lines.join('\n')}\n`;
}

function namespaces(indent: string, values: string[]): string {
  if (values.length === 0) {
    return `${indent}namespaces: []`;
  }
  return `${indent}namespaces:\n${values.map((v) => `${indent}  - ${v}`).join('\n')}`;
}
