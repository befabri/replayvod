import { RecordingSettingsFields } from "@/components/recording-settings-fields";
import type { useArchiveSettings } from "@/features/archive/settings";

export function ArchiveSettingsFields({
	controller,
	disabled = false,
}: {
	controller: ReturnType<typeof useArchiveSettings>;
	disabled?: boolean;
}) {
	const { settings } = controller;
	return (
		<RecordingSettingsFields
			tBase="videos"
			recordingType={settings.recording_type}
			quality={settings.quality}
			forceH264={settings.force_h264}
			onRecordingTypeChange={controller.setRecordingType}
			onQualityChange={controller.setQuality}
			onForceH264Change={controller.setForceH264}
			disabled={disabled}
		/>
	);
}
