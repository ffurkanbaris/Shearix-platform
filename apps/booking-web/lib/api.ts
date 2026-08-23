export class ApiError extends Error {
  public readonly retryable: boolean;
  constructor(public readonly status: number, message?: string, public readonly code?: string) {
    super(message ?? defaultError(status));
    this.name = "ApiError";
    this.retryable = status === 0 || status === 502 || status === 503 || status === 504;
  }
}

function defaultError(status: number): string {
  if (status === 400) return "Please check the information you entered.";
  if (status === 404) return "This booking page is unavailable.";
  if (status === 409) return "That time is no longer available.";
  if (status === 429) return "Too many requests. Please wait and try again.";
  if (status >= 500) return "The booking service is temporarily unavailable.";
  return "Something went wrong. Please try again.";
}

type RequestOptions = Omit<RequestInit, "body"> & { body?: unknown; signal?: AbortSignal };

async function message(response: Response): Promise<string | undefined> {
  if (!(response.headers.get("content-type") ?? "").includes("application/json")) return undefined;
  try {
    const payload = await response.json() as { error?: string; message?: string };
    return payload.message ?? payload.error;
  } catch {
    return undefined;
  }
}

export async function api<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const headers = new Headers(options.headers);
  let body: BodyInit | undefined;
  if (options.body !== undefined) {
    headers.set("content-type", "application/json");
    body = JSON.stringify(options.body);
  }
  let response: Response;
  try { response = await fetch(`/api${path}`, { ...options, headers, body, credentials: "include" }); }
  catch { throw new ApiError(0, "The booking service is temporarily unavailable.", "network_error"); }
  if (!response.ok) {
    const error = new ApiError(response.status, await message(response));
    if (response.status === 401 && typeof window !== "undefined") window.dispatchEvent(new Event("barber:customer-session-expired"));
    throw error;
  }
  if (response.status === 204) return undefined as T;
  return response.json() as Promise<T>;
}

export const apiClient = {
  get: <T>(path: string, signal?: AbortSignal) => api<T>(path, { method: "GET", signal }),
  post: <T>(path: string, body?: unknown, init?: RequestOptions) => api<T>(path, { ...init, method: "POST", body }),
  patch: <T>(path: string, body?: unknown, init?: RequestOptions) => api<T>(path, { ...init, method: "PATCH", body }),
};
