import { createContext, type ReactNode, useContext } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import type { ResumePolicy } from "@/features/videos/resume-policy";
import { useSettings } from "./queries";

const PlaybackContext = createContext<ResumePolicy | null>(null);

export function PlaybackSettingsProvider({
	children,
}: {
	children: ReactNode;
}) {
	const { t } = useTranslation();
	const { data, error, refetch, isFetching } = useSettings();
	if (data)
		return <PlaybackContext value={data.playback}>{children}</PlaybackContext>;
	if (error)
		return (
			<div role="alert" className="flex items-center gap-3 text-destructive">
				<span>{t("settings.failed_to_load")}</span>
				<Button disabled={isFetching} onClick={() => void refetch()}>
					{t("common.retry")}
				</Button>
			</div>
		);
	return (
		<div role="status" className="text-muted-foreground">
			{t("common.loading")}
		</div>
	);
}

export function usePlaybackSettings(): ResumePolicy {
	const policy = useContext(PlaybackContext);
	if (!policy)
		throw new Error(
			"Playback settings must be loaded before rendering playback consumers",
		);
	return policy;
}
