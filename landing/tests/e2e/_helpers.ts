import { execFileSync } from "node:child_process";
import path from "node:path";
import { fileURLToPath } from "node:url";

const HERE = path.dirname(fileURLToPath(import.meta.url));
const CLOUD_DIR = path.resolve(HERE, "../../../cloud");

// better-auth's magic-link plugin stores verification rows with
// identifier = token, value = JSON.stringify({email, name, attempt}).
// We pull them straight from D1 via wrangler so the e2e flow can drive a
// real magic link without hooking into the email module. Pass `remote=true`
// to query the deployed cloud worker's D1 (api.replayvod.com).
export function latestMagicLinkToken(
  email: string,
  opts: { remote?: boolean } = {},
): string {
  const stdout = execFileSync(
    "npx",
    [
      "wrangler",
      "d1",
      "execute",
      "replayvod-cloud",
      opts.remote ? "--remote" : "--local",
      "--json",
      "--command",
      "SELECT identifier, value FROM verification ORDER BY expires_at DESC LIMIT 50",
    ],
    { cwd: CLOUD_DIR, encoding: "utf-8" },
  );
  const parsed = JSON.parse(stdout) as Array<{
    results: Array<{ identifier: string; value: string }>;
  }>;
  const target = email.toLowerCase();
  for (const row of parsed[0]?.results ?? []) {
    try {
      const data = JSON.parse(row.value) as { email?: string };
      if (data.email?.toLowerCase() === target) return row.identifier;
    } catch {
      // not a magic-link row
    }
  }
  throw new Error(`no magic-link verification row for ${email}`);
}

// Apply the dev seed (idempotent) so every spec starts from a known fixture
// (dev@local.test exists with an active subscription tied to the monthly
// product configured in cloud/.dev.vars). Also wipes the relay_token table
// so token-creation specs don't trip over leftovers from prior dev runs.
export function applyDevSeed() {
  execFileSync("npm", ["run", "seed:local", "--silent"], {
    cwd: CLOUD_DIR,
    stdio: "inherit",
  });
  execFileSync(
    "npx",
    [
      "wrangler",
      "d1",
      "execute",
      "replayvod-cloud",
      "--local",
      "--command",
      "DELETE FROM relay_token; DELETE FROM session; DELETE FROM verification;",
    ],
    { cwd: CLOUD_DIR, stdio: "inherit" },
  );
}

export const SEEDED_EMAIL = "dev@local.test";
export const CLOUD_API = "http://localhost:8787";
export const RELAY = "http://localhost:8788";
