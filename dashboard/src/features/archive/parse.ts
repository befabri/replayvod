// Client-side mirrors of the server's VOD and channel parsers so a pasted
// list gets instant feedback. The server re-parses every input and is the
// authority; these only decide what to show before submit.

const TWITCH_HOSTS = /(^|\.)twitch\.tv$/i;
const VOD_PATH = /^\/(?:videos|[A-Za-z0-9_]+\/(?:video|v))\/(\d+)\/?$/i;
const LOGIN = /^[A-Za-z0-9_]{2,25}$/;

function parseTwitchUrl(raw: string): URL | null {
	const withScheme = raw.includes("://") ? raw : `https://${raw}`;
	try {
		const url = new URL(withScheme);
		return TWITCH_HOSTS.test(url.hostname) ? url : null;
	} catch {
		return null;
	}
}

/** The Twitch VOD id in a bare id or any twitch.tv VOD link, or null. */
export function parseVodId(input: string): string | null {
	const s = input.trim();
	if (!s) return null;
	if (/^\d+$/.test(s)) return s;
	const url = parseTwitchUrl(s);
	if (!url) return null;
	const match = VOD_PATH.exec(url.pathname);
	return match?.[1] ?? null;
}

/** The lowercase login in a channel name or twitch.tv channel link, or null. */
export function parseChannelInput(input: string): string | null {
	const s = input.trim().replace(/^@/, "");
	if (!s) return null;
	if (LOGIN.test(s)) return s.toLowerCase();
	const url = parseTwitchUrl(s);
	if (!url) return null;
	const [first, second] = url.pathname.split("/").filter(Boolean);
	if (!first || first === "videos" || first === "directory") return null;
	if (second === "video" || second === "v") return null;
	return LOGIN.test(first) ? first.toLowerCase() : null;
}

export type ParsedLine = { input: string; vodId: string | null };

/** Splits pasted text into non-empty lines (also on commas and spaces between links). */
export function parseVodLines(text: string): ParsedLine[] {
	return text
		.split(/[\n,]+|\s+(?=https?:\/\/|twitch\.tv)/)
		.map((line) => line.trim())
		.filter(Boolean)
		.map((input) => ({ input, vodId: parseVodId(input) }));
}
