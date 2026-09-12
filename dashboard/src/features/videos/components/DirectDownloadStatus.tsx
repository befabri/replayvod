import { ArrowRightIcon } from "@phosphor-icons/react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import type { DirectDownloadAvailability } from "@/features/videos/download-form";

export function DirectDownloadStatus({
	availability,
	onRecheck,
	onSwitchToSchedule,
}: {
	availability: DirectDownloadAvailability;
	onRecheck: () => void;
	onSwitchToSchedule?: () => void;
}) {
	const { t } = useTranslation();
	if (availability === "checking") {
		return (
			<p role="status" className="text-sm text-muted-foreground">
				{t("videos.download.checking_live")}
			</p>
		);
	}
	if (availability === "live") {
		return (
			<div
				role="status"
				className="flex items-center gap-2 rounded-md border border-border bg-muted/40 px-3 py-2 text-sm"
			>
				<span
					aria-hidden="true"
					className="size-2 shrink-0 animate-pulse rounded-full bg-destructive"
				/>
				<span className="font-medium">{t("videos.download.live_now")}</span>
				<span className="text-muted-foreground">
					· {t("videos.download.live_now_hint")}
				</span>
			</div>
		);
	}
	return (
		<div
			role={availability === "error" ? "alert" : "status"}
			className="space-y-2 rounded-md border border-border bg-muted/40 px-3 py-3 text-sm"
		>
			{availability === "error" ? (
				<p>{t("videos.download.live_check_failed")}</p>
			) : (
				<p>
					<span className="font-medium">{t("videos.download.offline")}</span>{" "}
					<span className="text-muted-foreground">
						· {t("videos.download.offline_hint")}
					</span>
				</p>
			)}
			<div className="flex flex-wrap items-center gap-2">
				<Button type="button" variant="outline" size="sm" onClick={onRecheck}>
					{t("videos.download.check_again")}
				</Button>
				{onSwitchToSchedule && (
					<Button
						type="button"
						variant="ghost"
						size="sm"
						onClick={onSwitchToSchedule}
					>
						{t("videos.download.set_up_schedule")}
						<ArrowRightIcon weight="bold" className="size-3.5" />
					</Button>
				)}
			</div>
		</div>
	);
}
