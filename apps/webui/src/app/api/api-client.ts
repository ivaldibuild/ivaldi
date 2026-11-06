import { HttpClient, HttpErrorResponse } from '@angular/common/http';
import { inject, Service } from '@angular/core';
import { firstValueFrom, map, Observable } from 'rxjs';
import { API_BASE_URL } from '../api-base-url';

/** Every `/v1` answer wraps its payload in `data`. */
interface Envelope<T> {
  status: string;
  data: T;
}

/** A page of a cursor-paged list. */
export interface Page<T> {
  items: T[];
}

/** A refusal from the control plane, carrying its `application/problem+json` detail. */
export class ApiError extends Error {
  readonly status: number;

  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

/** Calls the control plane's `/v1` operations and unwraps their envelopes. */
@Service()
export class ApiClient {
  private readonly http = inject(HttpClient);
  private readonly base = `${inject(API_BASE_URL)}/v1`;

  get<T>(path: string): Promise<T> {
    return unwrap(this.http.get<Envelope<T>>(`${this.base}${path}`));
  }

  /** The items of a cursor-paged list's first page. */
  async list<T>(path: string): Promise<T[]> {
    return (await this.get<Page<T>>(path)).items;
  }

  send<T>(method: 'POST' | 'PUT' | 'PATCH' | 'DELETE', path: string, body?: unknown): Promise<T> {
    return unwrap(this.http.request<Envelope<T> | null>(method, `${this.base}${path}`, { body }));
  }
}

async function unwrap<T>(request: Observable<Envelope<T> | null>): Promise<T> {
  try {
    return await firstValueFrom(request.pipe(map((envelope) => envelope?.data as T)));
  } catch (error) {
    throw toApiError(error);
  }
}

function toApiError(error: unknown): ApiError {
  if (!(error instanceof HttpErrorResponse)) {
    return new ApiError(0, 'Something went wrong. Try again.');
  }
  if (error.status === 0) {
    return new ApiError(0, "Ivaldi can't be reached. Check your connection and try again.");
  }
  if (error.status === 403) {
    return new ApiError(403, "You don't have permission to do this. Ask an owner.");
  }
  const problem = error.error as { detail?: string; errors?: { message?: string }[] } | null;
  const detail = problem?.errors?.[0]?.message ?? problem?.detail;
  if (error.status < 500 && detail) {
    return new ApiError(error.status, sentence(detail));
  }
  return new ApiError(error.status, 'Something went wrong on our side. Try again.');
}

function sentence(text: string): string {
  const trimmed = text.trim();
  const capitalised = trimmed.charAt(0).toUpperCase() + trimmed.slice(1);
  return /[.!?]$/.test(capitalised) ? capitalised : `${capitalised}.`;
}
