import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { formatAbsolute, formatTimestamp } from "@/lib/format-relative";

const TICK_INTERVAL_MS = 60_000;
const RELATIVE_WINDOW_MS = 7 * 24 * 3600 * 1000;

export function Timestamp({
	iso,
	className,
}: {
	iso: string;
	className?: string;
}) {
	const { i18n } = useTranslation();

	return (
		<TimestampValue iso={iso} locale={i18n.language} className={className} />
	);
}

export function TimestampValue({
	iso,
	locale,
	className,
}: {
	iso: string;
	locale: string;
	className?: string;
}) {
	const [, tick] = useState(0);

	useEffect(() => {
		const age = Date.now() - new Date(iso).getTime();
		if (age > RELATIVE_WINDOW_MS) return;
		const id = window.setInterval(() => tick((n) => n + 1), TICK_INTERVAL_MS);
		return () => window.clearInterval(id);
	}, [iso]);

	const visible = formatTimestamp(iso, locale);
	const full = formatAbsolute(iso, locale);
	return (
		<time dateTime={iso} title={full} className={className}>
			{visible}
		</time>
	);
}
