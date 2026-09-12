import { RecordingSettingsFields } from "@/components/recording-settings-fields";
import type { DirectDownloadController } from "@/features/videos/use-direct-download-form";
import { qualityTierForHeight } from "@/lib/recording-settings";
import { LiveRenditionPicker } from "./LiveRenditionPicker";

// A presentation binding only: the dialog's controller owns the query and
// resolves the same selection for these fields and for submission.
export function DirectDownloadFields({
	controller,
}: {
	controller: DirectDownloadController;
}) {
	const { form, values, quality, disabled } = controller;
	return (
		<RecordingSettingsFields
			tBase="videos"
			recordingType={values.recording_type}
			quality={
				typeof values.quality === "number"
					? qualityTierForHeight(values.quality)
					: values.quality
			}
			forceH264={values.force_h264}
			onRecordingTypeChange={(v) => form.setFieldValue("recording_type", v)}
			onQualityChange={(v) => form.setFieldValue("quality", v)}
			onForceH264Change={(v) => form.setFieldValue("force_h264", v)}
			disabled={disabled}
			qualityPicker={
				quality.kind === "ceiling"
					? undefined
					: ({ id, disabled }) => (
							<LiveRenditionPicker
								id={id}
								options={quality.kind === "rendition" ? quality.options : []}
								loading={quality.kind === "loading"}
								anonymous={quality.kind === "rendition" && quality.anonymous}
								value={quality.kind === "rendition" ? quality.height : null}
								onChange={(height) => form.setFieldValue("quality", height)}
								disabled={disabled}
							/>
						)
			}
		/>
	);
}
