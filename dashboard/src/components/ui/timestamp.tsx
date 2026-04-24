import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { formatAbsolute, formatTimestamp } from "@/lib/format-relative";

// Minute the relative label ticks on. A table mounted 30s ago and
// still on screen 90s later shouldn't keep reading "less than a
// minute ago" indefinitely — so we force a rerender once a minute
// on timestamps young enough to carry a relative label (<7 days).
// Older absolute stamps skip the tick since their output is stable.
const TICK_INTERVAL_MS = 60_000;
const RELATIVE_WINDOW_MS = 7 * 24 * 3600 * 1000;

// Timestamp renders an ISO datetime as a `<time>` element. The
// visible label picks between relative ("2h ago") for recent events
// and compact absolute ("4/24/26, 1:23 PM") for older ones;
// hovering always shows the full absolute stamp via the title
// attribute. Use this everywhere a timestamp is displayed so
// formatting, locale handling and the semantic element stay
// consistent.
export function Timestamp({
	iso,
	className,
}: {
	iso: string;
	className?: string;
}) {
	const { i18n } = useTranslation();
	const [, tick] = useState(0);

	useEffect(() => {
		const age = Date.now() - new Date(iso).getTime();
		if (age > RELATIVE_WINDOW_MS) return;
		const id = window.setInterval(() => tick((n) => n + 1), TICK_INTERVAL_MS);
		return () => window.clearInterval(id);
	}, [iso]);

	const visible = formatTimestamp(iso, i18n.language);
	const full = formatAbsolute(iso, i18n.language);
	return (
		<time dateTime={iso} title={full} className={className}>
			{visible}
		</time>
	);
}
