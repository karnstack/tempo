// Typed fetch wrapper over the openapi-typescript-generated paths.
// All HTTP calls in the SPA should go through these helpers so the
// request path / body / response stay in lock-step with the Go
// server's hand-rolled OpenAPI spec (see internal/api/openapi.yaml).

import type { paths } from "./openapi";

/** Concrete API base. Vite proxies /api -> :8080 in dev, same-origin in prod. */
const API_BASE = "/api/v1";

/** Any path the OpenAPI doc declares. */
export type ApiPath = keyof paths;

/** HTTP methods declared for a given path. */
type Methods<P extends ApiPath> = keyof paths[P] & ("get" | "post" | "delete" | "put" | "patch");

/** Successful JSON response type for (path, method). Falls back to unknown. */
type JsonResponse<P extends ApiPath, M extends Methods<P>> = paths[P][M] extends {
  responses: { 200: { content: { "application/json": infer J } } };
}
  ? J
  : paths[P][M] extends {
      responses: { 201: { content: { "application/json": infer J } } };
    }
  ? J
  : unknown;

/** Request body type for POST/PUT/PATCH endpoints. unknown when absent. */
type RequestBody<P extends ApiPath, M extends Methods<P>> = paths[P][M] extends {
  requestBody: { content: { "application/json": infer B } };
}
  ? B
  : never;

/** Path params (e.g. /repos/{owner}/{name}). undefined when absent. */
type PathParams<P extends ApiPath, M extends Methods<P>> = paths[P][M] extends {
  parameters: { path: infer Q };
}
  ? Q
  : undefined;

/** Query params (e.g. ?from=&to=). undefined when absent. */
type QueryParams<P extends ApiPath, M extends Methods<P>> = paths[P][M] extends {
  parameters: { query?: infer Q };
}
  ? Q
  : undefined;

export class ApiError extends Error {
  status: number;
  body: unknown;

  constructor(status: number, message: string, body: unknown) {
    super(message);
    this.status = status;
    this.body = body;
    this.name = "ApiError";
  }
}

interface FetchOptions<P extends ApiPath, M extends Methods<P>> {
  path?: PathParams<P, M>;
  query?: QueryParams<P, M>;
  body?: RequestBody<P, M>;
  signal?: AbortSignal;
}

function resolvePath(template: string, params?: Record<string, string | number>): string {
  if (!params) return template;
  return template.replace(/\{(\w+)\}/g, (_, key: string) => {
    const v = params[key];
    if (v === undefined || v === null) {
      throw new Error(`apiFetch: missing path param "${key}" for ${template}`);
    }
    return encodeURIComponent(String(v));
  });
}

function resolveQuery(params?: Record<string, string | number | undefined | null>): string {
  if (!params) return "";
  const usp = new URLSearchParams();
  for (const [k, v] of Object.entries(params)) {
    if (v === undefined || v === null) continue;
    usp.set(k, String(v));
  }
  const s = usp.toString();
  return s ? `?${s}` : "";
}

export async function apiFetch<P extends ApiPath, M extends Methods<P>>(
  path: P,
  method: M,
  opts: FetchOptions<P, M> = {},
): Promise<JsonResponse<P, M>> {
  const resolved =
    API_BASE + resolvePath(path as string, opts.path as Record<string, string | number> | undefined);
  const url = resolved + resolveQuery(opts.query as Record<string, string | number> | undefined);

  const init: RequestInit = {
    method: (method as string).toUpperCase(),
    credentials: "same-origin",
    headers: opts.body ? { "Content-Type": "application/json" } : undefined,
    signal: opts.signal,
  };
  if (opts.body !== undefined) {
    init.body = JSON.stringify(opts.body);
  }

  const res = await fetch(url, init);

  if (res.status === 204) {
    return undefined as JsonResponse<P, M>;
  }

  const ct = res.headers.get("Content-Type") ?? "";
  let parsed: unknown = null;
  if (ct.includes("application/json")) {
    parsed = await res.json();
  } else if (res.body) {
    parsed = await res.text();
  }

  if (!res.ok) {
    const message =
      parsed && typeof parsed === "object" && parsed !== null && "message" in parsed
        ? String((parsed as { message: unknown }).message)
        : `${res.status} ${res.statusText}`;
    throw new ApiError(res.status, message, parsed);
  }

  return parsed as JsonResponse<P, M>;
}

/** Convenience: GET. */
export function apiGet<P extends ApiPath>(
  path: P,
  opts: FetchOptions<P, "get" & Methods<P>> = {},
): Promise<JsonResponse<P, "get" & Methods<P>>> {
  return apiFetch(path, "get" as "get" & Methods<P>, opts);
}

/** Convenience: POST. */
export function apiPost<P extends ApiPath>(
  path: P,
  opts: FetchOptions<P, "post" & Methods<P>> = {},
): Promise<JsonResponse<P, "post" & Methods<P>>> {
  return apiFetch(path, "post" as "post" & Methods<P>, opts);
}

/** Convenience: DELETE. */
export function apiDelete<P extends ApiPath>(
  path: P,
  opts: FetchOptions<P, "delete" & Methods<P>> = {},
): Promise<JsonResponse<P, "delete" & Methods<P>>> {
  return apiFetch(path, "delete" as "delete" & Methods<P>, opts);
}
