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
import { QualityField } from "./QualityField";

export function CreateForm() {
	const { t } = useTranslation();
	const create = useCreateSchedule();

	const defaultValues: ScheduleFormValues = {
		broadcaster_id: "",
		quality: "HIGH",
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
			form.reset();
			toast.success(t("schedules.create_submit"));
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
			className="rounded-lg bg-card p-4 mb-6 space-y-3 shadow-sm"
		>
			<h2 className="text-lg font-medium">{t("schedules.create_title")}</h2>
			<div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
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
				<QualityField form={form} />
			</div>

			<FiltersFieldset form={form} />

			{create.isError && (
				<div className="rounded-md bg-destructive/10 p-3 text-destructive text-sm">
					{create.error?.message ?? t("schedules.create_failed")}
				</div>
			)}

			<form.Subscribe selector={(s) => [s.canSubmit, s.isSubmitting] as const}>
				{([canSubmit, isSubmitting]) => (
					<Button
						type="submit"
						disabled={!canSubmit || isSubmitting || create.isPending}
					>
						{isSubmitting || create.isPending
							? t("common.saving")
							: t("schedules.create_submit")}
					</Button>
				)}
			</form.Subscribe>
		</form>
	);
}
