import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";

// Cache Intl.RelativeTimeFormat instances per locale. The constructor is
// non-trivial (parses CLDR data), and formatRelative gets called once per
// row per render — we don't want to re-create it every time.
const rtfCache = new Map<string, Intl.RelativeTimeFormat>();

function getRtf(locale: string): Intl.RelativeTimeFormat {
	let rtf = rtfCache.get(locale);
	if (!rtf) {
		rtf = new Intl.RelativeTimeFormat(locale, { numeric: "auto" });
		rtfCache.set(locale, rtf);
	}
	return rtf;
}

// formatRelative returns a locale-aware "x minutes ago" string for the
// given ISO timestamp, using Intl.RelativeTimeFormat so we don't duplicate
// strings per locale in i18n bundles. The caller passes the current locale
// (from i18next) so switching languages updates on the next render.
export function formatRelative(iso: string, locale: string): string {
	const then = new Date(iso).getTime();
	const now = Date.now();
	const diffSec = Math.max(0, Math.round((now - then) / 1000));

	const rtf = getRtf(locale);

	if (diffSec < 60) return rtf.format(-diffSec, "second");
	const diffMin = Math.round(diffSec / 60);
	if (diffMin < 60) return rtf.format(-diffMin, "minute");
	const diffHr = Math.round(diffMin / 60);
	if (diffHr < 24) return rtf.format(-diffHr, "hour");
	const diffDay = Math.round(diffHr / 24);
	if (diffDay < 30) return rtf.format(-diffDay, "day");
	const diffMonth = Math.round(diffDay / 30);
	if (diffMonth < 12) return rtf.format(-diffMonth, "month");
	return rtf.format(-Math.round(diffMonth / 12), "year");
}

// formatAbsolute returns a compact locale-formatted date+time — short
// date (e.g. "4/24/26") and short time ("1:23 PM") — suitable for
// narrow table cells. Cached per-locale for the same reason as
// getRtf.
const dtfCache = new Map<string, Intl.DateTimeFormat>();
function getDtf(locale: string): Intl.DateTimeFormat {
	let dtf = dtfCache.get(locale);
	if (!dtf) {
		dtf = new Intl.DateTimeFormat(locale, {
			dateStyle: "short",
			timeStyle: "short",
		});
		dtfCache.set(locale, dtf);
	}
	return dtf;
}

export function formatAbsolute(iso: string, locale: string): string {
	return getDtf(locale).format(new Date(iso));
}

// formatTimestamp picks between relative and absolute: recent events
// (<7 days) read as "2h ago" since that's how people think about
// them; older events get a compact absolute stamp since "3 months
// ago" is less precise than the caller usually wants. The full ISO
// is available to the caller for `<time dateTime>` and title tooltip.
const RELATIVE_WINDOW_MS = 7 * 24 * 3600 * 1000;
export function formatTimestamp(iso: string, locale: string): string {
	const then = new Date(iso).getTime();
	if (Date.now() - then < RELATIVE_WINDOW_MS) {
		return formatRelative(iso, locale);
	}
	return formatAbsolute(iso, locale);
}

// useRelativeTime returns formatRelative(iso) and re-renders once a
// minute so the label doesn't freeze ("2h ago" turning into "3h ago"
// while the user keeps the page open). Mirrors the per-minute tick
// the Timestamp component uses, but exposes the raw string so callers
// can splice it into a translated template ("last stream {{when}}")
// instead of rendering a <time> element directly.
//
// Skips the interval for stamps older than the relative-window — at
// that point formatRelative would be replaced by a frozen absolute
// label anyway, and the tick would be wasted.
export function useRelativeTime(iso: string | undefined): string | undefined {
	const { i18n } = useTranslation();
	const [, tick] = useState(0);
	useEffect(() => {
		if (!iso) return;
		const age = Date.now() - new Date(iso).getTime();
		if (age > RELATIVE_WINDOW_MS) return;
		const id = window.setInterval(() => tick((n) => n + 1), 60_000);
		return () => window.clearInterval(id);
	}, [iso]);
	return iso ? formatRelative(iso, i18n.language) : undefined;
}
