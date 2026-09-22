import { useState } from "react";
import { useTranslation } from "react-i18next";

import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogHeader,
	DialogTitle,
	DialogTrigger,
} from "@/components/ui/dialog";
import { DirectDownloadForm } from "./DirectDownloadForm";

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
	return (
		<DialogContent>
			<DirectDownloadForm broadcasterId={broadcasterId} onClose={onClose}>
				<DialogHeader>
					<DialogTitle>{t("videos.trigger_title")}</DialogTitle>
					<DialogDescription>
						{t("videos.trigger_description", { name: broadcasterName })}
					</DialogDescription>
				</DialogHeader>
			</DirectDownloadForm>
		</DialogContent>
	);
}
