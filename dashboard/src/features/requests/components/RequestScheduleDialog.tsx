import { PlusIcon } from "@phosphor-icons/react";
import { useForm } from "@tanstack/react-form";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogHeader,
	DialogTitle,
	DialogTrigger,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { ChannelPicker } from "@/features/channels/components/ChannelPicker";
import { useCreateScheduleRequest } from "@/features/requests";

type RequestFormValues = {
	broadcaster_id: string;
	note: string;
};

export function RequestScheduleDialog({
	trigger,
}: {
	trigger?: React.ReactElement;
}) {
	const { t } = useTranslation();
	const [open, setOpen] = useState(false);

	return (
		<Dialog open={open} onOpenChange={setOpen}>
			<DialogTrigger
				render={
					trigger ?? (
						<Button>
							<PlusIcon weight="bold" />
							{t("requests.request_title")}
						</Button>
					)
				}
			/>
			{open && (
				<DialogContent className="max-w-md">
					<DialogHeader>
						<DialogTitle>{t("requests.request_title")}</DialogTitle>
						<DialogDescription>
							{t("requests.request_description")}
						</DialogDescription>
					</DialogHeader>
					<RequestForm onDone={() => setOpen(false)} />
				</DialogContent>
			)}
		</Dialog>
	);
}

function RequestForm({ onDone }: { onDone: () => void }) {
	const { t } = useTranslation();
	const create = useCreateScheduleRequest();

	const form = useForm({
		defaultValues: { broadcaster_id: "", note: "" } as RequestFormValues,
		onSubmit: async ({ value }) => {
			if (!value.broadcaster_id) return;
			const note = value.note.trim();
			try {
				await create.mutateAsync({
					broadcaster_id: value.broadcaster_id,
					note: note === "" ? undefined : note,
				});
				toast.success(t("requests.created"));
				onDone();
			} catch (err) {
				toast.error(
					err instanceof Error ? err.message : t("requests.create_failed"),
				);
			}
		},
	});

	return (
		<form
			onSubmit={(e) => {
				e.preventDefault();
				e.stopPropagation();
				void form.handleSubmit();
			}}
			className="flex flex-col gap-3"
		>
			<form.Field name="broadcaster_id">
				{(field) => (
					<ChannelPicker
						id={field.name}
						value={field.state.value}
						onChange={(id) => field.handleChange(id)}
						placeholder={t("requests.channel_placeholder")}
					/>
				)}
			</form.Field>
			<form.Field name="note">
				{(field) => (
					<Input
						type="text"
						value={field.state.value}
						onChange={(e) => field.handleChange(e.target.value)}
						maxLength={200}
						placeholder={t("requests.note_placeholder")}
					/>
				)}
			</form.Field>
			<form.Subscribe
				selector={(s) => [s.values.broadcaster_id, s.isSubmitting] as const}
			>
				{([broadcasterId, isSubmitting]) => (
					<Button
						type="submit"
						className="self-end"
						disabled={!broadcasterId || isSubmitting || create.isPending}
					>
						{isSubmitting || create.isPending
							? t("requests.creating")
							: t("requests.create")}
					</Button>
				)}
			</form.Subscribe>
		</form>
	);
}
