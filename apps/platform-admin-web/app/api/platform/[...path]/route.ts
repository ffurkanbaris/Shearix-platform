import { cookies } from "next/headers";
import { NextRequest, NextResponse } from "next/server";
import { cookieName, readSession } from "../../../../lib/session";
import { isSameOrigin } from "../../../../lib/origin";
async function proxy(request: NextRequest, context: { params: Promise<{ path: string[] }> }) {
  const session=readSession((await cookies()).get(cookieName)?.value); if(!session) return NextResponse.json({error:"unauthorized"},{status:401});
  if(!["GET","HEAD"].includes(request.method)&&!isSameOrigin(request.headers.get("origin"),request.headers.get("host")))return NextResponse.json({error:"forbidden"},{status:403});
  const {path}=await context.params; const base=(process.env.GATEWAY_URL??"http://gateway-service:8080").replace(/\/$/,"");
  const target=new URL(`${base}/api/v1/platform/${path.map(encodeURIComponent).join("/")}`); target.search=request.nextUrl.search;
  const headers=new Headers({"X-Platform-Admin-Token":process.env.PLATFORM_ADMIN_TOKEN??"","X-Platform-Actor":session.email,"Content-Type":request.headers.get("content-type")??"application/json"});
  const response=await fetch(target,{method:request.method,headers,body:["GET","HEAD"].includes(request.method)?undefined:await request.text(),cache:"no-store",signal:AbortSignal.timeout(10000)});
  return new NextResponse(await response.arrayBuffer(),{status:response.status,headers:{"content-type":response.headers.get("content-type")??"application/json"}});
}
export const GET=proxy; export const POST=proxy; export const PATCH=proxy;
