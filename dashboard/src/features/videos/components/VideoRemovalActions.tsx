import { useTranslation } from "react-i18next";
import type { VideoResponse } from "@/api/generated/trpc";
import { videoRemovalState } from "@/features/videos/removal";
import { RemoveVideoButton } from "./RemoveVideoButton";
import { RestoreVideoButton } from "./RestoreVideoButton";

// Both history and missing-media watch pages use the same eligibility rules.
// Callers check permission before mounting mutation observers for a recording.
export function VideoRemovalActions({
	video,
	withLabel = false,
}: {
	video: VideoResponse;
	withLabel?: boolean;
}) {
	const { t } = useTranslation();
	const state = videoRemovalState(video);
	if (state === "pending") {
		return (
			<span role="status" className="text-xs text-muted-foreground">
				{t("videos.removal_pending")}
			</span>
		);
	}
	if (state !== "restorable" && state !== "removable") return null;
	return (
		<>
			{state === "restorable" && (
				<RestoreVideoButton videoId={video.id} withLabel={withLabel} />
			)}
			<RemoveVideoButton
				videoId={video.id}
				permanent={state === "restorable"}
				withLabel={withLabel}
			/>
		</>
	);
}
