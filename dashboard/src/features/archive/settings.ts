import { useState } from "react";
import type { RecordingMode, RecordingQuality } from "@/lib/recording-settings";
import { forceH264For } from "@/lib/recording-settings";

export type ArchiveSettings = {
	recording_type: RecordingMode;
	quality: RecordingQuality;
	force_h264: boolean;
};

export const DEFAULT_ARCHIVE_SETTINGS: ArchiveSettings = {
	recording_type: "video",
	quality: "HIGH",
	force_h264: false,
};

export function useArchiveSettings(initial = DEFAULT_ARCHIVE_SETTINGS) {
	const [settings, setSettings] = useState<ArchiveSettings>(initial);
	return {
		settings,
		setRecordingType: (recording_type: RecordingMode) =>
			setSettings((s) => ({
				...s,
				recording_type,
				force_h264: forceH264For(recording_type, s.force_h264),
			})),
		setQuality: (quality: RecordingQuality) =>
			setSettings((s) => ({ ...s, quality })),
		setForceH264: (force_h264: boolean) =>
			setSettings((s) => ({
				...s,
				force_h264: forceH264For(s.recording_type, force_h264),
			})),
	};
}

export function archivePayloadSettings(settings: ArchiveSettings) {
	return {
		recording_type: settings.recording_type,
		quality: settings.quality,
		force_h264: forceH264For(settings.recording_type, settings.force_h264),
	};
}
