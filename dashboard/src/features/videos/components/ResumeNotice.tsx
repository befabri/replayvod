import { XIcon } from "@phosphor-icons/react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { formatPlaybackTime } from "@/features/videos/format";
import { cn } from "@/lib/utils";

export function ResumeNotice({
	offsetSeconds,
	onStartOver,
	onDismiss,
	className,
}: {
	offsetSeconds: number;
	onStartOver: () => void;
	onDismiss: () => void;
	className?: string;
}) {
	const { t } = useTranslation();
	return (
		<div
			role="status"
			data-testid="resume-notice"
			className={cn(
				"pointer-events-auto flex min-w-0 items-center gap-2 rounded-lg border border-border bg-card px-3 py-2 text-sm text-foreground",
				className,
			)}
		>
			<span className="min-w-0 flex-1 truncate">
				{t("watch.resumed_from", { time: formatPlaybackTime(offsetSeconds) })}
			</span>
			<Button type="button" variant="outline" size="xs" onClick={onStartOver}>
				{t("watch.start_over")}
			</Button>
			<Button
				type="button"
				variant="ghost"
				size="icon-xs"
				onClick={onDismiss}
				aria-label={t("watch.dismiss_resume")}
			>
				<XIcon />
			</Button>
		</div>
	);
}
