import { NextRequest } from "next/server";
import { proxyGateway } from "../../../../shared/server-proxy";

export const runtime = "nodejs";
async function proxy(request: NextRequest, context: { params: Promise<{ path: string[] }> }) {
  return proxyGateway(request, (await context.params).path);
}
export const GET = proxy; export const POST = proxy; export const PUT = proxy; export const PATCH = proxy; export const DELETE = proxy;
