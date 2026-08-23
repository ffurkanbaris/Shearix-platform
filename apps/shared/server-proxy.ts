export type ProxyConfig = { gatewayURL: string; timeoutMs: number; maxResponseBytes: number; maxRequestBytes: number };

const requestHeaders = ["accept", "accept-language", "content-type", "cookie", "idempotency-key", "if-match", "if-none-match"];
const responseHeaders = ["content-type", "cache-control", "location", "retry-after", "vary"];

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
  let config: ProxyConfig; try { config = proxyConfig(); } catch { return proxyError(500, "proxy_configuration", "The service is temporarily unavailable."); }
  const url = new URL(`/api/${path.map(encodeURIComponent).join("/")}`, config.gatewayURL); url.search = new URL(request.url).search;
  const declared = Number(request.headers.get("content-length")); if (Number.isFinite(declared) && declared > config.maxRequestBytes) return proxyError(413, "request_too_large", "The request is too large.");
  const timeout = new AbortController(); const timer = setTimeout(() => timeout.abort(), config.timeoutMs); const signal = AbortSignal.any([request.signal, timeout.signal]);
  try {
    const method = request.method.toUpperCase(); const requestBody = method === "GET" || method === "HEAD" ? undefined : await boundedRequestBody(request, config.maxRequestBytes);
    const upstream = await transport(url, { method, headers: proxyRequestHeaders(request.headers), body: requestBody, signal });
    const responseBody = await boundedBody(upstream, config.maxResponseBytes); const headers = allowedResponseHeaders(upstream);
    if ([204, 205, 304].includes(upstream.status)) return new Response(null, { status: upstream.status, headers });
    const responseData = responseBody.buffer.slice(responseBody.byteOffset, responseBody.byteOffset + responseBody.byteLength) as ArrayBuffer;
    return new Response(responseData, { status: upstream.status, headers });
  } catch (error) {
    if (timeout.signal.aborted) return proxyError(504, "gateway_timeout", "The service is taking too long to respond. Please try again.");
    if (request.signal.aborted) return proxyError(499, "request_cancelled", "The request was cancelled.");
    if (error instanceof Error && error.message === "request_too_large") return proxyError(413, "request_too_large", "The request is too large.");
    if (error instanceof Error && error.message === "response_too_large") return proxyError(502, "gateway_response_too_large", "The service returned an invalid response.");
    return proxyError(502, "gateway_unavailable", "The service is temporarily unavailable. Please try again.");
  } finally { clearTimeout(timer); }
}
import { request as httpRequest } from "node:http";
import { request as httpsRequest } from "node:https";
import { Readable } from "node:stream";
