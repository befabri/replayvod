import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";

const rtfCache = new Map<string, Intl.RelativeTimeFormat>();

function getRtf(locale: string): Intl.RelativeTimeFormat {
	let rtf = rtfCache.get(locale);
	if (!rtf) {
		rtf = new Intl.RelativeTimeFormat(locale, { numeric: "auto" });
		rtfCache.set(locale, rtf);
	}
	return rtf;
}

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

const RELATIVE_WINDOW_MS = 7 * 24 * 3600 * 1000;
export function formatTimestamp(iso: string, locale: string): string {
	const then = new Date(iso).getTime();
	if (Date.now() - then < RELATIVE_WINDOW_MS) {
		return formatRelative(iso, locale);
	}
	return formatAbsolute(iso, locale);
}

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

export function formatUntil(iso: string, locale: string): string {
	const then = new Date(iso).getTime();
	const diffSec = Math.max(0, Math.round((then - Date.now()) / 1000));
	const rtf = getRtf(locale);
	if (diffSec < 60) return rtf.format(diffSec, "second");
	const diffMin = Math.round(diffSec / 60);
	if (diffMin < 60) return rtf.format(diffMin, "minute");
	const diffHr = Math.round(diffMin / 60);
	if (diffHr < 24) return rtf.format(diffHr, "hour");
	return rtf.format(Math.round(diffHr / 24), "day");
}

export function useUntil(iso: string | undefined): string | undefined {
	const { i18n } = useTranslation();
	const [, tick] = useState(0);
	useEffect(() => {
		if (!iso) return;
		const id = window.setInterval(() => tick((n) => n + 1), 30_000);
		return () => window.clearInterval(id);
	}, [iso]);
	return iso ? formatUntil(iso, i18n.language) : undefined;
}
