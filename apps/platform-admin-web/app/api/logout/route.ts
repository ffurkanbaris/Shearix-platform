import { NextResponse } from "next/server";
import { cookieName, cookieOptions } from "../../../lib/session";
export async function POST() { const response=NextResponse.json({ok:true}); response.cookies.set(cookieName,"",{...cookieOptions,maxAge:0}); return response; }
