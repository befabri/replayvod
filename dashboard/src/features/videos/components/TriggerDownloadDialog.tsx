import { useState } from "react";
import { useTranslation } from "react-i18next";

import { Button } from "@/components/ui/button";
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogFooter,
	DialogHeader,
	DialogTitle,
	DialogTrigger,
} from "@/components/ui/dialog";
import {
	buildDirectDownloadPayload,
	useDirectDownloadForm,
} from "@/features/videos/download-form";
import { useTriggerDownload } from "@/features/videos/queries";
import { DirectDownloadFields } from "./DirectDownloadFields";

// TriggerDownloadDialog wraps its children with a dialog trigger; the
// form inside submits video.triggerDownload with the operator's mode +
// codec choices. Backend wires recording_type + force_h264 onto the
// videos row; the HLS pipeline will consume them once the native
// downloader lands. See .docs/spec/download-pipeline.md "Recording
// modes" + "UI surface". The channel detail page uses the richer,
// tabbed ChannelDownloadDialog instead; this stays for the per-video
// "record live" entry point.
export function TriggerDownloadDialog({
	broadcasterId,
	broadcasterName,
	children,
}: {
	broadcasterId: string;
	broadcasterName: string;
	children: React.ReactNode;
}) {
	const [open, setOpen] = useState(false);

	return (
		<Dialog open={open} onOpenChange={setOpen}>
			<DialogTrigger render={children as React.ReactElement} />
			{open && (
				<TriggerDownloadDialogBody
					broadcasterId={broadcasterId}
					broadcasterName={broadcasterName}
					onClose={() => setOpen(false)}
				/>
			)}
		</Dialog>
	);
}

function TriggerDownloadDialogBody({
	broadcasterId,
	broadcasterName,
	onClose,
}: {
	broadcasterId: string;
	broadcasterName: string;
	onClose: () => void;
}) {
	const { t } = useTranslation();
	const trigger = useTriggerDownload();

	const form = useDirectDownloadForm(async (value) => {
		await trigger.mutateAsync(buildDirectDownloadPayload(broadcasterId, value));
		onClose();
	});

	return (
		<DialogContent>
			<form
				onSubmit={(e) => {
					e.preventDefault();
					e.stopPropagation();
					void form.handleSubmit();
				}}
				className="space-y-5"
			>
				<DialogHeader>
					<DialogTitle>{t("videos.trigger_title")}</DialogTitle>
					<DialogDescription>
						{t("videos.trigger_description", { name: broadcasterName })}
					</DialogDescription>
				</DialogHeader>

				<DirectDownloadFields form={form} />

				{trigger.isError && (
					<div className="rounded-md bg-destructive/10 border border-destructive/20 p-3 text-destructive text-sm">
						{trigger.error?.message ?? t("videos.trigger_failed")}
					</div>
				)}

				<DialogFooter>
					<Button
						type="button"
						variant="outline"
						onClick={onClose}
						disabled={trigger.isPending}
					>
						{t("common.cancel")}
					</Button>
					<form.Subscribe
						selector={(s) => [s.canSubmit, s.isSubmitting] as const}
					>
						{([canSubmit, isSubmitting]) => (
							<Button
								type="submit"
								disabled={!canSubmit || isSubmitting || trigger.isPending}
							>
								{isSubmitting || trigger.isPending
									? t("common.saving")
									: t("videos.trigger_submit")}
							</Button>
						)}
					</form.Subscribe>
				</DialogFooter>
			</form>
		</DialogContent>
	);
}
