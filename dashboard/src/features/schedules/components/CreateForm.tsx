import { useTranslation } from "react-i18next";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { ChannelPicker } from "@/features/channels/components/ChannelPicker";
import {
	buildSchedulePayload,
	useScheduleForm,
} from "@/features/schedules/form";
import { useCreateSchedule } from "@/features/schedules/queries";
import type { ScheduleFormValues } from "@/features/schedules/schema";
import { FieldError } from "./FieldError";
import { FiltersFieldset } from "./FiltersFieldset";
import { RecordingSettingsField } from "./RecordingSettingsField";

// CreateForm is the schedule creation form, rendered inside the
// CreateScheduleDialog modal. It mirrors EditForm's modal layout (vertical
// fields, bled footer) but starts from empty defaults and exposes the
// channel picker, since the target channel isn't fixed the way it is when
// editing an existing schedule.
export function CreateForm({ onDone }: { onDone: () => void }) {
	const { t } = useTranslation();
	const create = useCreateSchedule();

	const defaultValues: ScheduleFormValues = {
		broadcaster_id: "",
		recording_type: "video",
		quality: "HIGH",
		force_h264: false,
		has_min_viewers: false,
		min_viewers: undefined,
		has_categories: false,
		category_ids: [],
		has_tags: false,
		tag_ids: [],
		is_delete_rediff: false,
		time_before_delete: undefined,
	};

	const form = useScheduleForm(defaultValues, async (value) => {
		try {
			await create.mutateAsync({
				...buildSchedulePayload(value),
				broadcaster_id: value.broadcaster_id.trim(),
				is_disabled: false,
			});
			toast.success(t("schedules.created"));
			onDone();
		} catch (err) {
			toast.error(
				err instanceof Error ? err.message : t("schedules.create_failed"),
			);
		}
	});

	return (
		<form
			onSubmit={(e) => {
				e.preventDefault();
				e.stopPropagation();
				void form.handleSubmit();
			}}
			className="flex flex-col gap-4"
		>
			<form.Field name="broadcaster_id">
				{(field) => (
					<div className="flex flex-col gap-1">
						<Label htmlFor={field.name} className="text-muted-foreground">
							{t("schedules.broadcaster_id")}
						</Label>
						<ChannelPicker
							id={field.name}
							value={field.state.value}
							onChange={(id) => field.handleChange(id)}
							aria-invalid={
								field.state.meta.errors.length > 0 ? true : undefined
							}
						/>
						<FieldError errors={field.state.meta.errors} />
					</div>
				)}
			</form.Field>

			<RecordingSettingsField form={form} />

			<FiltersFieldset form={form} />

			{create.isError && (
				<div className="rounded-md bg-destructive/10 p-3 text-destructive text-sm">
					{create.error?.message ?? t("schedules.create_failed")}
				</div>
			)}

			{/* Footer mirrors EditForm: bled to the dialog edges, Cancel + Create
				on the right with a shared min-width so labels line up. */}
			<div className="flex items-center justify-end gap-2 border-t border-border pt-4 -mx-6 px-6 -mb-6 pb-6">
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
							disabled={!canSubmit || isSubmitting || create.isPending}
							className="min-w-24"
						>
							{isSubmitting || create.isPending
								? t("common.saving")
								: t("schedules.create_submit")}
						</Button>
					)}
				</form.Subscribe>
			</div>
		</form>
	);
}
