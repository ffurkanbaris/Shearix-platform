// GATEWAY_REQUEST_TIMEOUT_MS (default 10s below) must stay slightly above the
// gateway service's own outbound timeout (GATEWAY_OUTBOUND_TIMEOUT_MS,
// default 9s - see services/gateway-service/internal/config) so the gateway's
// 504 on a slow backend is reached before this proxy's own timeout races it.
// If this default is lowered, lower the gateway's default with it so it stays
// comfortably below this value; the two are independently configurable but
// meant to be tuned together.
export type ProxyConfig = { gatewayURL: string; timeoutMs: number; maxResponseBytes: number; maxRequestBytes: number };

const requestHeaders = ["accept", "accept-language", "content-type", "cookie", "idempotency-key", "if-match", "if-none-match"];
const responseHeaders = ["content-type", "cache-control", "location", "retry-after", "vary"];

// Mirrors platform/httpx.isBoundedToken on the Go side: a caller-supplied
// X-Request-ID is only ever forwarded when it is a short, safe token — never
// unbounded, never containing characters that could inject a header or break
// log/metric formatting downstream. An invalid or absent value always gets a
// freshly generated one instead of being dropped silently.
const REQUEST_ID_PATTERN = /^[A-Za-z0-9._:-]{1,128}$/;

function resolveRequestID(source: Headers): string {
  const supplied = source.get("x-request-id");
  if (supplied && REQUEST_ID_PATTERN.test(supplied)) return supplied;
  return crypto.randomUUID();
}

function positive(value: string | undefined, fallback: number, name: string, maximum: number): number {
  if (value === undefined || value === "") return fallback;
  if (!/^\d+$/.test(value)) throw new Error(`${name} must be a positive integer`);
  const parsed = Number(value);
  if (!Number.isSafeInteger(parsed) || parsed <= 0 || parsed > maximum) throw new Error(`${name} is outside its supported range`);
  return parsed;
}

export function proxyConfig(env: Partial<NodeJS.ProcessEnv> = process.env): ProxyConfig {
  const gatewayURL = env.GATEWAY_URL ?? "http://gateway-service:8080";
  try { new URL(gatewayURL); } catch { throw new Error("GATEWAY_URL must be an absolute URL"); }
  return { gatewayURL: gatewayURL.replace(/\/$/, ""), timeoutMs: positive(env.GATEWAY_REQUEST_TIMEOUT_MS, 10_000, "GATEWAY_REQUEST_TIMEOUT_MS", 60_000), maxResponseBytes: positive(env.GATEWAY_MAX_RESPONSE_BYTES, 1_048_576, "GATEWAY_MAX_RESPONSE_BYTES", 16 * 1_048_576), maxRequestBytes: positive(env.GATEWAY_MAX_REQUEST_BYTES, 1_048_576, "GATEWAY_MAX_REQUEST_BYTES", 16 * 1_048_576) };
}

export function proxyRequestHeaders(source: Headers): Headers {
  const headers = new Headers();
  for (const name of requestHeaders) { const value = source.get(name); if (value) headers.set(name, value); }
  // Host is routing data only. Tenant and internal-auth headers are never copied.
  const host = source.get("host"); if (host) headers.set("host", host);
  return headers;
}

function proxyError(status: number, code: string, message: string): Response { return Response.json({ error: code, message }, { status, headers: { "cache-control": "no-store" } }); }

async function boundedBody(response: Response, limit: number): Promise<Uint8Array> {
  if (response.body === null) return new Uint8Array();
  const declared = Number(response.headers.get("content-length"));
  if (Number.isFinite(declared) && declared > limit) throw new Error("response_too_large");
  const reader = response.body.getReader(); const chunks: Uint8Array[] = []; let length = 0;
  try { while (true) { const part = await reader.read(); if (part.done) break; length += part.value.byteLength; if (length > limit) { await reader.cancel(); throw new Error("response_too_large"); } chunks.push(part.value); } } finally { reader.releaseLock(); }
  const body = new Uint8Array(length); let offset = 0; for (const chunk of chunks) { body.set(chunk, offset); offset += chunk.byteLength; } return body;
}

async function boundedRequestBody(request: Request, limit: number): Promise<ArrayBuffer | undefined> {
  if (request.body === null) return undefined;
  const reader = request.body.getReader(); const chunks: Uint8Array[] = []; let length = 0;
  try {
    while (true) {
      if (request.signal.aborted) throw new Error("request_cancelled");
      const part = await reader.read();
      if (part.done) break;
      length += part.value.byteLength;
      if (length > limit) { await reader.cancel(); throw new Error("request_too_large"); }
      chunks.push(part.value);
    }
  } finally { reader.releaseLock(); }
  const body = new Uint8Array(length); let offset = 0;
  for (const chunk of chunks) { body.set(chunk, offset); offset += chunk.byteLength; }
  return body.buffer;
}

function allowedResponseHeaders(upstream: Response): Headers {
  const headers = new Headers();
  for (const name of responseHeaders) { const value = upstream.headers.get(name); if (value) headers.set(name, value); }
  const cookies = (upstream.headers as Headers & { getSetCookie?: () => string[] }).getSetCookie?.() ?? [];
  for (const cookie of cookies) headers.append("set-cookie", cookie);
  return headers;
}

// Node's fetch implementation does not allow callers to override Host. The
// gateway deliberately resolves tenants from Host, while the connection itself
// uses the Docker service name. Use the Node client so those two values can be
// different without trusting a client-supplied tenant header.
export type ProxyTransport = (url: URL, init: RequestInit) => Promise<Response>;

function fetchPreservingHost(url: URL, init: RequestInit): Promise<Response> {
  return new Promise((resolve, reject) => {
    const transport = url.protocol === "https:" ? httpsRequest : httpRequest;
    const headers: Record<string, string> = {};
    new Headers(init.headers).forEach((value, name) => { headers[name] = value; });
    const outgoing = transport(url, { method: init.method, headers, signal: init.signal ?? undefined }, (incoming) => {
      const responseHeaders = new Headers();
      for (let index = 0; index < incoming.rawHeaders.length; index += 2) {
        responseHeaders.append(incoming.rawHeaders[index], incoming.rawHeaders[index + 1]);
      }
      const status = incoming.statusCode ?? 502;
      const body = [204, 205, 304].includes(status) ? null : Readable.toWeb(incoming) as ReadableStream<Uint8Array>;
      resolve(new Response(body, { status, statusText: incoming.statusMessage, headers: responseHeaders }));
    });
    outgoing.on("error", reject);
    if (init.body instanceof ArrayBuffer) outgoing.write(new Uint8Array(init.body));
    else if (ArrayBuffer.isView(init.body)) outgoing.write(new Uint8Array(init.body.buffer, init.body.byteOffset, init.body.byteLength));
    else if (typeof init.body === "string") outgoing.write(init.body);
    outgoing.end();
  });
}

export async function proxyGateway(request: Request, path: string[], transport: ProxyTransport = fetchPreservingHost): Promise<Response> {
  // Resolved once, up front, so every possible return path below — success,
  // every error branch, even the pre-flight config/size checks — carries the
  // exact same X-Request-ID. This is the one value the gateway's own
  // httpx.RequestID() will see and preserve (rather than generating its own),
  // giving true browser-to-backend correlation end to end.
  const requestId = resolveRequestID(request.headers);
  const withRequestID = (response: Response): Response => {
    response.headers.set("x-request-id", requestId);
    return response;
  };

  let config: ProxyConfig; try { config = proxyConfig(); } catch { return withRequestID(proxyError(500, "proxy_configuration", "The service is temporarily unavailable.")); }
  const url = new URL(`/api/${path.map(encodeURIComponent).join("/")}`, config.gatewayURL); url.search = new URL(request.url).search;
  const declared = Number(request.headers.get("content-length")); if (Number.isFinite(declared) && declared > config.maxRequestBytes) return withRequestID(proxyError(413, "request_too_large", "The request is too large."));
  const timeout = new AbortController(); const timer = setTimeout(() => timeout.abort(), config.timeoutMs); const signal = AbortSignal.any([request.signal, timeout.signal]);
  try {
    const method = request.method.toUpperCase(); const requestBody = method === "GET" || method === "HEAD" ? undefined : await boundedRequestBody(request, config.maxRequestBytes);
    const outboundHeaders = proxyRequestHeaders(request.headers); outboundHeaders.set("x-request-id", requestId);
    const upstream = await transport(url, { method, headers: outboundHeaders, body: requestBody, signal });
    const responseBody = await boundedBody(upstream, config.maxResponseBytes); const headers = allowedResponseHeaders(upstream);
    if ([204, 205, 304].includes(upstream.status)) return withRequestID(new Response(null, { status: upstream.status, headers }));
    const responseData = responseBody.buffer.slice(responseBody.byteOffset, responseBody.byteOffset + responseBody.byteLength) as ArrayBuffer;
    return withRequestID(new Response(responseData, { status: upstream.status, headers }));
  } catch (error) {
    if (timeout.signal.aborted) return withRequestID(proxyError(504, "gateway_timeout", "The service is taking too long to respond. Please try again."));
    if (request.signal.aborted) return withRequestID(proxyError(499, "request_cancelled", "The request was cancelled."));
    if (error instanceof Error && error.message === "request_too_large") return withRequestID(proxyError(413, "request_too_large", "The request is too large."));
    if (error instanceof Error && error.message === "response_too_large") return withRequestID(proxyError(502, "gateway_response_too_large", "The service returned an invalid response."));
    return withRequestID(proxyError(502, "gateway_unavailable", "The service is temporarily unavailable. Please try again."));
  } finally { clearTimeout(timer); }
}
import { request as httpRequest } from "node:http";
import { request as httpsRequest } from "node:https";
import { Readable } from "node:stream";
