import { apiErrorMessage } from "@/lib/format";

export class ApiError extends Error {
  public readonly retryable: boolean;
  public readonly code?: string;
  constructor(
    public readonly status: number,
    message?: string,
    code?: string,
  ) {
    super(apiErrorMessage(status, message));
    this.name = "ApiError";
    this.code = code;
    this.retryable = status === 0 || status === 502 || status === 503 || status === 504;
  }
}

type RequestOptions = Omit<RequestInit, "body"> & {
  body?: unknown;
  signal?: AbortSignal;
};

async function errorDetail(response: Response): Promise<string | undefined> {
  const contentType = response.headers.get("content-type") ?? "";
  if (!contentType.includes("application/json")) return undefined;
  try {
    const body = (await response.json()) as { error?: string; message?: string };
    return body.message ?? body.error;
  } catch {
    return undefined;
  }
}

export async function api<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const headers = new Headers(options.headers);
  let body: BodyInit | undefined;
  if (options.body !== undefined) {
    headers.set("Content-Type", "application/json");
    body = JSON.stringify(options.body);
  }
  let response: Response;
  try { response = await fetch(`/api${path}`, { ...options, headers, body, credentials: "include" }); }
  catch { throw new ApiError(0, "The service is temporarily unavailable.", "network_error"); }
  if (!response.ok) {
    const detail = await errorDetail(response);
    const error = new ApiError(response.status, detail);
    if (response.status === 401 && typeof window !== "undefined") window.dispatchEvent(new Event("barber:session-expired"));
    throw error;
  }
  if (response.status === 204) return undefined as T;
  return (await response.json()) as T;
}

export const apiClient = {
  get: <T>(path: string, signal?: AbortSignal) => api<T>(path, { method: "GET", signal }),
  post: <T>(path: string, body?: unknown, init?: RequestOptions) => api<T>(path, { ...init, method: "POST", body }),
  patch: <T>(path: string, body?: unknown, init?: RequestOptions) => api<T>(path, { ...init, method: "PATCH", body }),
  put: <T>(path: string, body?: unknown, init?: RequestOptions) => api<T>(path, { ...init, method: "PUT", body }),
  delete: <T>(path: string, init?: RequestOptions) => api<T>(path, { ...init, method: "DELETE" }),
};
