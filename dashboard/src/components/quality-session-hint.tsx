import { WarningCircleIcon } from "@phosphor-icons/react";
import { Link } from "@tanstack/react-router";
import { useSelector } from "@tanstack/react-store";
import { useTranslation } from "react-i18next";
import { useTwitchPlaybackStatus } from "@/features/system/queries";
import {
	qualityAboveHD,
	type RecordingQuality,
} from "@/lib/recording-settings";
import { authStore, hasRole } from "@/stores/auth";

export function useIsOwner() {
	return hasRole(
		useSelector(authStore, (s) => s.user),
		"owner",
	);
}

export function ViewerSessionNotice({
	text,
	testId = "session-notice",
}: {
	text: string;
	testId?: string;
}) {
	return (
		<p data-testid={testId} className="text-xs text-muted-foreground">
			{text}
		</p>
	);
}

export function OwnerSessionNotice({
	text,
	state,
	testId = "session-notice",
}: {
	text: string;
	state?: string;
	testId?: string;
}) {
	const { t } = useTranslation();
	return (
		<p
			role="status"
			data-testid={testId}
			data-state={state}
			className="flex items-start gap-2 rounded-md border border-yellow-500/30 bg-yellow-500/10 px-3 py-2 text-sm text-foreground"
		>
			<WarningCircleIcon
				className="mt-0.5 size-4 shrink-0 text-yellow-500"
				aria-hidden
			/>
			<span>
				{text}{" "}
				<Link
					to="/dashboard/system/twitch"
					className="underline underline-offset-2 hover:text-foreground"
				>
					{t("twitch_playback.quality_session_link")}
				</Link>
			</span>
		</p>
	);
}

export function QualitySessionHint({ quality }: { quality: RecordingQuality }) {
	const isOwner = useIsOwner();
	if (!qualityAboveHD(quality)) return null;
	return isOwner ? <OwnerHint /> : <ViewerHint />;
}

function ViewerHint() {
	const { t } = useTranslation();
	return (
		<ViewerSessionNotice
			testId="quality-session-hint"
			text={t("twitch_playback.quality_session_viewer")}
		/>
	);
}

function OwnerHint() {
	const { t } = useTranslation();
	const { data } = useTwitchPlaybackStatus();
	const state = data?.state;
	if (state !== "disconnected" && state !== "reconnect_required") return null;
	return (
		<OwnerSessionNotice
			testId="quality-session-hint"
			state={state}
			text={t(
				state === "disconnected"
					? "twitch_playback.quality_session_missing"
					: "twitch_playback.quality_session_reconnect",
			)}
		/>
	);
}
