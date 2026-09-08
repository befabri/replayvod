import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import type { MediaProbeResult } from "@/features/videos/playback";
import { cn } from "@/lib/utils";

export type MediaFailureKind = Exclude<MediaProbeResult, "ok">;

// MediaUnavailablePanel replaces the player once a source cannot play, so the
// page never sits on a spinner. actions is the route's slot for the remove
// button and history link, shown for the two definitive kinds only.
export function MediaUnavailablePanel({
	kind,
	onRetry,
	actions,
	compact = false,
}: {
	kind: MediaFailureKind;
	onRetry: () => void;
	actions?: ReactNode;
	compact?: boolean;
}) {
	const { t } = useTranslation();
	return (
		<section
			role="alert"
			data-testid="media-unavailable"
			data-kind={kind}
			className={cn(
				"flex flex-col items-center justify-center gap-3 rounded-lg bg-black px-6 text-center text-white shadow-sm",
				compact ? "py-8" : "aspect-video",
			)}
		>
			<p className="text-base font-medium">{t(`watch.media_${kind}_title`)}</p>
			<p className="max-w-md text-sm">{t(`watch.media_${kind}_body`)}</p>
			<div className="mt-1 flex flex-wrap items-center justify-center gap-3">
				{kind !== "removed" ? (
					<Button variant="outline" size="sm" onClick={onRetry}>
						{t("watch.retry")}
					</Button>
				) : null}
				{kind !== "failed" ? actions : null}
			</div>
		</section>
	);
}
