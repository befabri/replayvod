import { TRPCClientError, type TRPCLink } from "@trpc/client";
import { observable } from "@trpc/server/observable";
import type { AppRouter } from "@/api/trpc";

export const CREDENTIAL_TRANSPORT_ERROR =
	"Use HTTPS for both the dashboard and API before connecting a Twitch session. HTTP is allowed only on this computer (localhost).";

function safeEndpoint(url: URL) {
	if (url.username || url.password) return false;
	if (url.protocol === "https:") return true;
	if (url.protocol !== "http:") return false;
	const host = url.hostname;
	return (
		host === "localhost" ||
		host === "[::1]" ||
		/^127(?:\.\d{1,3}){3}$/.test(host)
	);
}

export function credentialTransportAllowed(pageURL: string, apiURL: string) {
	try {
		const page = new URL(pageURL);
		return (
			safeEndpoint(page) && safeEndpoint(new URL(apiURL || page.origin, page))
		);
	} catch {
		return false;
	}
}

export function browserCredentialTransportAllowed(apiURL: string) {
	return (
		typeof window !== "undefined" &&
		window.isSecureContext === true &&
		credentialTransportAllowed(window.location.href, apiURL)
	);
}

export function credentialTransportLink(
	allowed: () => boolean,
): TRPCLink<AppRouter> {
	return () =>
		({ op, next }) => {
			if (op.path !== "twitchPlayback.connect" || allowed()) return next(op);
			return observable((observer) => {
				observer.error(new TRPCClientError(CREDENTIAL_TRANSPORT_ERROR));
			});
		};
}
