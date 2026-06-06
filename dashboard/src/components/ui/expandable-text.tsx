import { useLayoutEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { cn } from "@/lib/utils";

export function ExpandableText({
	children,
	className,
}: {
	children: React.ReactNode;
	className?: string;
}) {
	const { t } = useTranslation();
	const [expanded, setExpanded] = useState(false);
	const [overflows, setOverflows] = useState(false);
	const textRef = useRef<HTMLParagraphElement>(null);

	useLayoutEffect(() => {
		const el = textRef.current;
		if (!el) return;
		const measure = () => setOverflows(el.scrollHeight > el.clientHeight + 1);
		measure();
		const observer = new ResizeObserver(measure);
		observer.observe(el);
		return () => observer.disconnect();
	}, []);

	return (
		<div>
			<p ref={textRef} className={cn(className, !expanded && "line-clamp-3")}>
				{children}
			</p>
			{(overflows || expanded) && (
				<button
					type="button"
					onClick={() => setExpanded((v) => !v)}
					className="mt-1 text-link text-sm font-medium hover:underline"
				>
					{expanded ? t("common.show_less") : t("common.show_more")}
				</button>
			)}
		</div>
	);
}
