import { inject, Service } from '@angular/core';
import { ApiClient } from '../api/api-client';
import { AgentStatus, Cluster, IssuedEnrolment } from '../api/models';

/** A Cluster's fields; the slug is derived from the name when left out. */
export interface ClusterFields {
  display_name: string;
  slug?: string;
  runtime?: 'kubernetes';
}

/** The Clusters a Kubernetes agent serves, and their enrolment. */
@Service()
export class ClustersApi {
  private readonly api = inject(ApiClient);

  clusters(includeArchived = false): Promise<Cluster[]> {
    const query = includeArchived ? '?include_archived=true' : '';
    return this.api.list(`/clusters${query}`);
  }

  cluster(id: string): Promise<Cluster> {
    return this.api.get(`/clusters/${id}`);
  }

  createCluster(fields: ClusterFields): Promise<Cluster> {
    return this.api.send('POST', '/clusters', { runtime: 'kubernetes', ...fields });
  }

  updateCluster(id: string, displayName: string): Promise<Cluster> {
    return this.api.send('PATCH', `/clusters/${id}`, { display_name: displayName });
  }

  archiveCluster(id: string): Promise<Cluster> {
    return this.api.send('POST', `/clusters/${id}/archive`);
  }

  restoreCluster(id: string): Promise<Cluster> {
    return this.api.send('POST', `/clusters/${id}/restore`);
  }

  agentStatus(id: string): Promise<AgentStatus> {
    return this.api.get(`/clusters/${id}/agent`);
  }

  createEnrolment(
    id: string,
    method: 'bearer' | 'kubernetes',
  ): Promise<IssuedEnrolment> {
    return this.api.send('POST', `/clusters/${id}/enrolments`, { method });
  }
}
