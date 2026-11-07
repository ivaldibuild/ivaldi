import { Artifact, Channel, Release, ReleaseStatus } from '../../api/models';

export const statusLabels: Record<ReleaseStatus, string> = {
  draft: 'Draft',
  published: 'Published',
  withdrawn: 'Withdrawn',
};

/** A digest shortened for a table, such as sha256:4f9c2e1a7b30. The full digest goes in a title. */
export function shortDigest(digest: string): string {
  const [algorithm, hex] = digest.includes(':') ? digest.split(':', 2) : ['', digest];
  return algorithm ? `${algorithm}:${hex.slice(0, 12)}` : hex.slice(0, 12);
}

export function channelName(channels: Channel[], id: string): string {
  return channels.find((c) => c.id === id)?.display_name ?? 'Unknown Channel';
}

/** Each Build in a Release, named by its artifact and labelled by version or digest. */
export function buildSummaries(
  release: Release,
  artifacts: Artifact[],
): { artifact: string; label: string; digest: string }[] {
  return release.builds.map((item) => ({
    artifact: artifacts.find((a) => a.id === item.artifact_id)?.name ?? item.build.artifact_name,
    label: item.build.version ?? shortDigest(item.build.digest),
    digest: item.build.digest,
  }));
}
