import { useTranslation } from "react-i18next";
import { RecordingSettingsFields } from "@/components/recording-settings-fields";
import { cn } from "@/lib/utils";
import type { ScheduleFormApi } from "../form";

// RecordingSettingsField is the schedule-form binding for the shared
// RecordingSettingsFields control: it wraps the fields in the labelled
// fieldset and wires them to the schedule form.
export function RecordingSettingsField({
	form,
	className,
}: {
	form: ScheduleFormApi;
	className?: string;
}) {
	const { t } = useTranslation();

	return (
		<fieldset className={cn("space-y-3", className)}>
			<legend className="text-xs text-muted-foreground pb-1">
				{t("schedules.recording_settings")}
			</legend>
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
						tBase="schedules"
						recordingType={recordingType}
						quality={quality}
						forceH264={forceH264}
						onRecordingTypeChange={(v) =>
							form.setFieldValue("recording_type", v)
						}
						onQualityChange={(v) => form.setFieldValue("quality", v)}
						onForceH264Change={(v) => form.setFieldValue("force_h264", v)}
					/>
				)}
			</form.Subscribe>
		</fieldset>
	);
}
