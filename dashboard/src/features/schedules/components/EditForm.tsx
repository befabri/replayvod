import { useForm } from "@tanstack/react-form";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";
import { Avatar } from "@/components/ui/avatar";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import {
	Select,
	SelectContent,
	SelectItem,
	SelectTrigger,
	SelectValue,
} from "@/components/ui/select";
import { useChannel } from "@/features/channels";
import type { ScheduleResponse } from "@/features/schedules";
import {
	useDeleteSchedule,
	useUpdateSchedule,
} from "@/features/schedules/queries";
import {
	ScheduleFormSchema,
	type ScheduleFormValues,
} from "@/features/schedules/schema";
import { FiltersFieldset } from "./FiltersFieldset";

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

	const form = useForm({
		defaultValues: {
			broadcaster_id: schedule.broadcaster_id,
			quality: schedule.quality as ScheduleFormValues["quality"],
			has_min_viewers: schedule.has_min_viewers,
			min_viewers: schedule.min_viewers ?? undefined,
			has_categories: schedule.has_categories,
			category_ids: schedule.categories.map((c) => c.id),
			has_tags: schedule.has_tags,
			tag_ids: schedule.tags.map((tag) => tag.id),
			is_delete_rediff: schedule.is_delete_rediff,
			time_before_delete: schedule.time_before_delete ?? undefined,
		} as ScheduleFormValues,
		validators: {
			onSubmit: ScheduleFormSchema,
		},
		onSubmit: async ({ value }) => {
			try {
				await update.mutateAsync({
					id: schedule.id,
					quality: value.quality,
					has_min_viewers: value.has_min_viewers,
					min_viewers: value.has_min_viewers ? value.min_viewers : undefined,
					has_categories: value.has_categories,
					has_tags: value.has_tags,
					is_delete_rediff: value.is_delete_rediff,
					time_before_delete: value.is_delete_rediff
						? value.time_before_delete
						: undefined,
					is_disabled: schedule.is_disabled,
					category_ids: value.has_categories ? value.category_ids : [],
					tag_ids: value.has_tags ? value.tag_ids : [],
				});
				toast.success(t("schedules.save"));
				onDone();
			} catch (err) {
				toast.error(
					err instanceof Error ? err.message : t("schedules.update_failed"),
				);
			}
		},
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

			<form.Field name="quality">
				{(field) => (
					<div className="flex flex-col gap-1 max-w-xs">
						<Label htmlFor={field.name} className="text-muted-foreground">
							{t("schedules.quality")}
						</Label>
						<Select
							value={field.state.value}
							onValueChange={(v) =>
								field.handleChange(v as ScheduleFormValues["quality"])
							}
						>
							<SelectTrigger id={field.name}>
								<SelectValue />
							</SelectTrigger>
							<SelectContent>
								<SelectItem value="HIGH">
									{t("schedules.quality_high")}
								</SelectItem>
								<SelectItem value="MEDIUM">
									{t("schedules.quality_medium")}
								</SelectItem>
								<SelectItem value="LOW">
									{t("schedules.quality_low")}
								</SelectItem>
							</SelectContent>
						</Select>
					</div>
				)}
			</form.Field>

			<FiltersFieldset form={form} />

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
