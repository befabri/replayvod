import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { DialogFooter } from "@/components/ui/dialog";
import { useTriggerDownload } from "@/features/videos/queries";
import { useDirectDownloadForm } from "@/features/videos/use-direct-download-form";
import { DirectDownloadFields } from "./DirectDownloadFields";
import { DirectDownloadStatus } from "./DirectDownloadStatus";

// Own the live check, mutation, error state and submit controls once. Dialogs
// supply their header; both surfaces enforce the same live-channel boundary.
export function DirectDownloadForm({
	broadcasterId,
	onClose,
	onSwitchToSchedule,
	children,
}: {
	broadcasterId: string;
	onClose: () => void;
	onSwitchToSchedule?: () => void;
	children?: ReactNode;
}) {
	const { t } = useTranslation();
	const trigger = useTriggerDownload();
	const controller = useDirectDownloadForm({
		broadcasterId,
		onSubmit: async (payload) => {
			await trigger.mutateAsync(payload);
			toast.success(t("videos.triggered"));
			onClose();
		},
	});
	const { form } = controller;
	return (
		<form
			onSubmit={(event) => {
				event.preventDefault();
				event.stopPropagation();
				// TanStack Form retains failure state; the mutation renders its error.
				void form.handleSubmit().catch(() => {});
			}}
			className="space-y-5"
		>
			{children}
			<DirectDownloadStatus
				availability={controller.availability}
				onRecheck={controller.recheck}
				onSwitchToSchedule={onSwitchToSchedule}
			/>
			<DirectDownloadFields controller={controller} />
			{trigger.isError && (
				<div
					role="alert"
					className="rounded-md border border-destructive/20 bg-destructive/10 p-3 text-sm text-destructive"
				>
					{trigger.error.message || t("videos.trigger_failed")}
				</div>
			)}
			<form.Subscribe
				selector={(state) => [state.canSubmit, state.isSubmitting] as const}
			>
				{([canSubmit, isSubmitting]) => (
					<DialogFooter>
						<Button
							type="button"
							variant="outline"
							onClick={onClose}
							disabled={isSubmitting}
						>
							{t("common.cancel")}
						</Button>
						<Button
							type="submit"
							disabled={!controller.ready || !canSubmit || isSubmitting}
						>
							{isSubmitting ? t("common.saving") : t("videos.trigger_submit")}
						</Button>
					</DialogFooter>
				)}
			</form.Subscribe>
		</form>
	);
}
