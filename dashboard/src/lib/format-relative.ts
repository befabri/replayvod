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
