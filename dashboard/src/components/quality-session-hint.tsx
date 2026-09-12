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

// useIsOwner reports whether the signed-in user can manage the Twitch
// playback session; the session notices below choose their wording on it.
export function useIsOwner() {
	return hasRole(
		useSelector(authStore, (s) => s.user),
		"owner",
	);
}

// ViewerSessionNotice is the muted line shown to admins and viewers about the
// playback session: they can neither read its state nor change it.
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

// OwnerSessionNotice is the amber advisory shown to the owner, linking to the
// page where the playback session is managed.
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

// QualitySessionHint sits under the quality ladder and speaks up when the
// chosen ceiling reaches above what Twitch serves an anonymous viewer (Up to
// 1440p, No limit). The recorder silently takes the best rendition it can
// get, so without this the user picks 1440p and finds a 1080p file.
//
// Owners get a warning tied to the real connection state, with a link to the
// page that fixes it; twitchPlayback.status is owner-only, so nobody else can
// read it. Everyone else gets a note that the owner holds the session, since
// they can neither check it nor change it. Nothing renders for the owner
// while the status loads or once a session is connected.
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
