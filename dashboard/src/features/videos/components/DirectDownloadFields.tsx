import { RecordingSettingsFields } from "@/components/recording-settings-fields";
import type { DirectDownloadFormApi } from "@/features/videos/download-form";

// DirectDownloadFields is the direct-download binding for the shared
// RecordingSettingsFields control, used by the standalone download dialog and
// the "Download now" tab of the channel dialog. `disabled` greys everything out
// at once; the channel dialog passes it when the channel is offline, since a
// direct download can only record a live stream.
export function DirectDownloadFields({
	form,
	disabled = false,
}: {
	form: DirectDownloadFormApi;
	disabled?: boolean;
}) {
	return (
		<form.Subscribe
			selector={(s) =>
				[
					s.values.recording_type,
					s.values.quality,
					s.values.force_h264,
				] as const
			}
		>
			{([recordingType, quality, forceH264]) => (
				<RecordingSettingsFields
					tBase="videos"
					recordingType={recordingType}
					quality={quality}
					forceH264={forceH264}
					onRecordingTypeChange={(v) => form.setFieldValue("recording_type", v)}
					onQualityChange={(v) => form.setFieldValue("quality", v)}
					onForceH264Change={(v) => form.setFieldValue("force_h264", v)}
					disabled={disabled}
				/>
			)}
		</form.Subscribe>
	);
}
