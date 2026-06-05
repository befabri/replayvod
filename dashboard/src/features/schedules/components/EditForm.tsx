import { useTranslation } from "react-i18next";
import { toast } from "sonner";
import { Avatar } from "@/components/ui/avatar";
import { Button } from "@/components/ui/button";
import { useChannel } from "@/features/channels";
import type { ScheduleResponse } from "@/features/schedules";
import {
	buildSchedulePayload,
	useScheduleForm,
} from "@/features/schedules/form";
import { scheduleQualityValue } from "@/features/schedules/quality";
import {
	useDeleteSchedule,
	useUpdateSchedule,
} from "@/features/schedules/queries";
import { scheduleRecordingTypeValue } from "@/features/schedules/recording";
import type { ScheduleFormValues } from "@/features/schedules/schema";
import { FiltersFieldset } from "./FiltersFieldset";
import { RecordingSettingsField } from "./RecordingSettingsField";

// EditForm mirrors the v1 shared ScheduleForm in edit mode: the
// broadcaster field is shown read-only at the top (context, no change
// allowed after creation); the footer carries Delete on the left and
// Cancel + Save on the right, matching v1's modal footer layout.
export function EditForm({
	schedule,
	onDone,
}: {
	schedule: ScheduleResponse;
	onDone: () => void;
}) {
	const { t } = useTranslation();
	const update = useUpdateSchedule();
	const del = useDeleteSchedule();
	const { data: channel } = useChannel(schedule.broadcaster_id);

	const defaultValues: ScheduleFormValues = {
		broadcaster_id: schedule.broadcaster_id,
		recording_type: scheduleRecordingTypeValue(schedule.recording_type),
		quality: scheduleQualityValue(schedule.quality),
		force_h264: schedule.force_h264,
		has_min_viewers: schedule.has_min_viewers,
		min_viewers: schedule.min_viewers ?? undefined,
		has_categories: schedule.has_categories,
		category_ids: schedule.categories.map((c) => c.id),
		has_tags: schedule.has_tags,
		tag_ids: schedule.tags.map((tag) => tag.id),
		is_delete_rediff: schedule.is_delete_rediff,
		time_before_delete: schedule.time_before_delete ?? undefined,
	};

	const form = useScheduleForm(defaultValues, async (value) => {
		try {
			await update.mutateAsync({
				...buildSchedulePayload(value),
				id: schedule.id,
				is_disabled: schedule.is_disabled,
			});
			toast.success(t("schedules.save"));
			onDone();
		} catch (err) {
			toast.error(
				err instanceof Error ? err.message : t("schedules.update_failed"),
			);
		}
	});

	const handleDelete = async () => {
		try {
			await del.mutateAsync({ id: schedule.id });
			toast.success(t("schedules.deleted"));
			onDone();
		} catch (err) {
			toast.error(
				err instanceof Error ? err.message : t("schedules.update_failed"),
			);
		}
	};

	const channelLabel = channel?.broadcaster_name ?? schedule.broadcaster_id;

	return (
		<form
			onSubmit={(e) => {
				e.preventDefault();
				e.stopPropagation();
				void form.handleSubmit();
			}}
			className="flex flex-col gap-4"
		>
			{/* Broadcaster context — read-only in edit, matching v1. */}
			<div className="flex items-center gap-3">
				<Avatar
					src={channel?.profile_image_url}
					name={channelLabel}
					alt={channelLabel}
					size="md"
				/>
				<div className="flex flex-col min-w-0">
					<span className="text-sm font-medium truncate">{channelLabel}</span>
					<span className="text-xs text-muted-foreground font-mono truncate">
						{schedule.broadcaster_id}
					</span>
				</div>
			</div>

			<RecordingSettingsField form={form} />

			<FiltersFieldset form={form} initialCategories={schedule.categories} />

			{update.isError && (
				<div className="rounded-md bg-destructive/10 p-3 text-destructive text-sm">
					{update.error?.message ?? t("schedules.update_failed")}
				</div>
			)}

			{/* Footer: Delete on the left, Cancel + Save on the right (v1 modal layout).
				All three buttons share a min-width so they line up visually across
				translations with varied label lengths. */}
			<div className="flex items-center justify-between gap-3 border-t border-border pt-4 -mx-6 px-6 -mb-6 pb-6">
				<Button
					type="button"
					variant="destructive"
					onClick={handleDelete}
					disabled={del.isPending}
					className="min-w-24"
				>
					{t("schedules.delete")}
				</Button>
				<div className="flex gap-2">
					<Button
						type="button"
						variant="outline"
						onClick={onDone}
						className="min-w-24"
					>
						{t("schedules.cancel")}
					</Button>
					<form.Subscribe
						selector={(s) => [s.canSubmit, s.isSubmitting] as const}
					>
						{([canSubmit, isSubmitting]) => (
							<Button
								type="submit"
								disabled={!canSubmit || isSubmitting || update.isPending}
								className="min-w-24"
							>
								{isSubmitting || update.isPending
									? t("common.saving")
									: t("schedules.save")}
							</Button>
						)}
					</form.Subscribe>
				</div>
			</div>
		</form>
	);
}
