import { handleLogin } from "../../../lib/login-handler";
export async function POST(request: Request) { return handleLogin(request); }
