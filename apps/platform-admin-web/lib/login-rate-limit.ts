import { createHash } from "node:crypto";
import { createClient, type RedisClientType } from "redis";

const script = `
local n=redis.call('INCR',KEYS[1])
if n==1 then redis.call('PEXPIRE',KEYS[1],ARGV[1]) end
if n>tonumber(ARGV[2]) then return 0 end
return 1`;

export const loginLimit = 5;
export const loginWindowMs = 15 * 60 * 1000;

export interface LimitStore {
  allow(key: string, limit: number, windowMs: number): Promise<boolean>;
}

class RedisLimitStore implements LimitStore {
  private client?: RedisClientType;

  private async connectedClient() {
    if (!this.client) {
      const url = process.env.REDIS_URL;
      if (!url) throw new Error("REDIS_URL is required for platform login rate limiting");
      this.client = createClient({ url, socket: { connectTimeout: 1_000, reconnectStrategy: false } });
      this.client.on("error", () => { /* The caller fails closed; never log connection secrets. */ });
    }
    if (!this.client.isOpen) await this.client.connect();
    return this.client;
  }

  async allow(key: string, limit: number, windowMs: number) {
    const client = await this.connectedClient();
    const result = await client.eval(script, { keys: [key], arguments: [String(windowMs), String(limit)] });
    return Number(result) === 1;
  }
}

const redisStore = new RedisLimitStore();
const digest = (value: string) => createHash("sha256").update(value).digest("hex");

export async function allowPlatformLogin(email: string, address: string, store: LimitStore = redisStore) {
  const accountKey = digest(email.trim().toLocaleLowerCase("en-US"));
  const addressKey = digest(address || "unknown");
  try {
    const ipAllowed = await store.allow(`platform-login:ip:${addressKey}`, loginLimit, loginWindowMs);
    const accountAllowed = await store.allow(`platform-login:account:${accountKey}`, loginLimit, loginWindowMs);
    return { allowed: ipAllowed && accountAllowed, unavailable: false };
  } catch {
    return { allowed: false, unavailable: true };
  }
}

export function requestAddress(request: Request) {
  return request.headers.get("x-platform-client-ip")?.trim() || "unknown";
}
