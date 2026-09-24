import { useTranslation } from "react-i18next";
import { toast } from "sonner";
import { Alert } from "@/components/ui/alert";
import { Avatar } from "@/components/ui/avatar";
import { Button } from "@/components/ui/button";
import {
	Dialog,
	DialogContent,
	DialogHeader,
	DialogTitle,
} from "@/components/ui/dialog";
import { Label } from "@/components/ui/label";
import type { ScheduleRequestResponse } from "@/features/requests";
import { useApproveScheduleRequest } from "@/features/requests";
import { FieldError } from "@/features/schedules/components/FieldError";
import { FiltersFieldset } from "@/features/schedules/components/FiltersFieldset";
import { RecordingSettingsField } from "@/features/schedules/components/RecordingSettingsField";
import {
	buildSchedulePayload,
	useScheduleForm,
} from "@/features/schedules/form";
import type { ScheduleFormValues } from "@/features/schedules/schema";

export function ApproveRequestDialog({
	request,
	onClose,
}: {
	request: ScheduleRequestResponse | null;
	onClose: () => void;
}) {
	return (
		<Dialog open={request !== null} onOpenChange={(open) => !open && onClose()}>
			{request && (
				<DialogContent className="max-w-xl">
					<ApproveRequestDialogBody request={request} onClose={onClose} />
				</DialogContent>
			)}
		</Dialog>
	);
}

function ApproveRequestDialogBody({
	request,
	onClose,
}: {
	request: ScheduleRequestResponse;
	onClose: () => void;
}) {
	const { t } = useTranslation();
	const approve = useApproveScheduleRequest();

	const defaultValues: ScheduleFormValues = {
		broadcaster_id: request.broadcaster_id,
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
			await approve.mutateAsync({
				request_id: request.id,
				...buildSchedulePayload(value),
				is_disabled: false,
			});
			toast.success(t("requests.approved"));
			onClose();
		} catch (err) {
			toast.error(
				err instanceof Error ? err.message : t("requests.approve_failed"),
			);
		}
	});

	return (
		<>
			<DialogHeader>
				<DialogTitle>{t("requests.approve_title")}</DialogTitle>
			</DialogHeader>
			<form
				onSubmit={(e) => {
					e.preventDefault();
					e.stopPropagation();
					void form.handleSubmit();
				}}
				className="flex flex-col gap-4"
			>
				<div className="flex flex-col gap-1">
					<Label className="text-muted-foreground">
						{t("schedules.broadcaster_id")}
					</Label>
					<div className="flex items-center gap-2">
						<Avatar
							src={request.profile_image_url}
							name={request.broadcaster_name}
							size="md"
						/>
						<span>{request.broadcaster_name}</span>
						<span className="text-xs text-muted-foreground">
							@{request.broadcaster_login}
						</span>
					</div>
				</div>

				{request.note && (
					<div className="flex flex-col gap-1">
						<Label className="text-muted-foreground">
							{t("requests.col_note")}
						</Label>
						<p className="text-sm">{request.note}</p>
					</div>
				)}

				<form.Field name="broadcaster_id">
					{(field) => <FieldError errors={field.state.meta.errors} />}
				</form.Field>

				<RecordingSettingsField form={form} />

				<FiltersFieldset form={form} />

				{approve.isError && (
					<Alert variant="destructive">
						{approve.error?.message ?? t("requests.approve_failed")}
					</Alert>
				)}

				<div className="flex items-center justify-end gap-2 border-t border-border pt-4 -mx-6 px-6 -mb-6 pb-6">
					<Button
						type="button"
						variant="outline"
						onClick={onClose}
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
								disabled={!canSubmit || isSubmitting || approve.isPending}
								className="min-w-24"
							>
								{isSubmitting || approve.isPending
									? t("common.saving")
									: t("requests.approve")}
							</Button>
						)}
					</form.Subscribe>
				</div>
			</form>
		</>
	);
}
