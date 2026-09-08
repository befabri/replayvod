import { useForm } from "@tanstack/react-form";
import { useCallback, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import {
	Card,
	CardContent,
	CardDescription,
	CardHeader,
	CardTitle,
} from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { QueryTable } from "@/components/ui/query-table";
import {
	Select,
	SelectContent,
	SelectItem,
	SelectTrigger,
	SelectValue,
} from "@/components/ui/select";
import type { InviteCreatedInfo } from "@/features/invites";
import { useCreateInvite, useInvites } from "@/features/invites";
import { inviteColumns } from "@/features/invites/components/columns";

type InviteFormValues = {
	role: "viewer" | "admin";
	ttl_minutes: string;
	note: string;
};

// TTL choices are presets rather than free input: invites are short-lived
// onboarding links, not standing credentials.
const TTL_MINUTES = ["60", "1440", "10080", "43200"] as const;
// A note names who the invite is for; the server enforces the same cap.
const NOTE_MAX = 60;

// InvitesSection is the invite management block on the users page:
// create form, copy-once URL panel, and the invites table.
export function InvitesSection() {
	const { t } = useTranslation();
	const invites = useInvites();
	const create = useCreateInvite();
	// The panel shows the most recently issued link, whether it came from
	// the create form or a row's "New link" action.
	const [issued, setIssued] = useState<InviteCreatedInfo | null>(null);
	const onRevoked = useCallback(
		(id: number) => setIssued((cur) => (cur?.id === id ? null : cur)),
		[],
	);
	const columns = useMemo(
		() => inviteColumns(t, { onIssued: setIssued, onRevoked }),
		[t, onRevoked],
	);

	const roleItems = [
		{ value: "viewer", label: t("users.role_viewer") },
		{ value: "admin", label: t("users.role_admin") },
	];
	const ttlLabels: Record<(typeof TTL_MINUTES)[number], string> = {
		"60": t("invites.ttl_1h"),
		"1440": t("invites.ttl_24h"),
		"10080": t("invites.ttl_7d"),
		"43200": t("invites.ttl_30d"),
	};
	const ttlItems = TTL_MINUTES.map((value) => ({
		value,
		label: ttlLabels[value],
	}));

	const form = useForm({
		defaultValues: {
			role: "viewer",
			ttl_minutes: "1440",
			note: "",
		} as InviteFormValues,
		onSubmit: async ({ value }) => {
			const note = value.note.trim();
			try {
				setIssued(
					await create.mutateAsync({
						role: value.role,
						ttl_minutes: Number(value.ttl_minutes),
						note: note === "" ? undefined : note,
					}),
				);
			} catch {
				// The mutation's error state renders below; keep the form
				// available for retry without rejecting the submit handler.
			}
		},
	});

	const copyUrl = async (url: string) => {
		try {
			await navigator.clipboard.writeText(url);
			toast.success(t("invites.url_copied"));
		} catch {
			toast.error(t("invites.url_copy_failed"));
		}
	};

	return (
		<section className="mt-10">
			<h2 className="text-xl font-medium mb-4">{t("invites.title")}</h2>

			<Card className="mb-6">
				<CardHeader>
					<CardTitle>{t("invites.create")}</CardTitle>
					<CardDescription>{t("invites.description")}</CardDescription>
				</CardHeader>
				<CardContent>
					<form
						onSubmit={(e) => {
							e.preventDefault();
							e.stopPropagation();
							void form.handleSubmit();
						}}
						className="flex flex-wrap items-end gap-3"
					>
						<form.Field name="role">
							{(field) => (
								<div className="flex w-36 flex-col gap-1">
									<Label htmlFor="invite-role">{t("invites.col_role")}</Label>
									<Select
										items={roleItems}
										value={field.state.value}
										onValueChange={(v) =>
											field.handleChange(v as InviteFormValues["role"])
										}
									>
										<SelectTrigger id="invite-role">
											<SelectValue />
										</SelectTrigger>
										<SelectContent>
											{roleItems.map(({ value, label }) => (
												<SelectItem key={value} value={value}>
													{label}
												</SelectItem>
											))}
										</SelectContent>
									</Select>
								</div>
							)}
						</form.Field>
						<form.Field name="ttl_minutes">
							{(field) => (
								<div className="flex w-36 flex-col gap-1">
									<Label htmlFor="invite-ttl">{t("invites.field_ttl")}</Label>
									<Select
										items={ttlItems}
										value={field.state.value}
										onValueChange={(v) => field.handleChange(String(v))}
									>
										<SelectTrigger id="invite-ttl">
											<SelectValue />
										</SelectTrigger>
										<SelectContent>
											{ttlItems.map(({ value, label }) => (
												<SelectItem key={value} value={value}>
													{label}
												</SelectItem>
											))}
										</SelectContent>
									</Select>
								</div>
							)}
						</form.Field>
						<form.Field name="note">
							{(field) => (
								<div className="flex min-w-56 flex-1 flex-col gap-1">
									<div className="flex items-baseline justify-between">
										<Label htmlFor="invite-note">{t("invites.col_note")}</Label>
										<span className="text-xs text-muted-foreground">
											{field.state.value.length}/{NOTE_MAX}
										</span>
									</div>
									<Input
										id="invite-note"
										type="text"
										value={field.state.value}
										onChange={(e) => field.handleChange(e.target.value)}
										maxLength={NOTE_MAX}
										placeholder={t("invites.note_placeholder")}
									/>
								</div>
							)}
						</form.Field>
						<form.Subscribe
							selector={(s) => [s.canSubmit, s.isSubmitting] as const}
						>
							{([canSubmit, isSubmitting]) => (
								<Button
									type="submit"
									disabled={!canSubmit || isSubmitting || create.isPending}
								>
									{isSubmitting || create.isPending
										? t("invites.creating")
										: t("invites.create")}
								</Button>
							)}
						</form.Subscribe>
					</form>

					{create.isError && (
						<div className="mt-4 rounded-md bg-destructive/10 border border-destructive/20 p-3 text-destructive text-sm">
							{create.error?.message ?? t("invites.failed_to_create")}
						</div>
					)}
				</CardContent>
			</Card>

			{issued && (
				<div className="mb-6 rounded-md border border-border bg-card p-3">
					<p className="text-sm mb-2">{t("invites.url_ready")}</p>
					<div className="flex items-center gap-2">
						<code className="flex-1 truncate rounded-md bg-background border border-border px-2 py-1.5 text-sm">
							{issued.url}
						</code>
						<Button
							type="button"
							variant="outline"
							onClick={() => void copyUrl(issued.url)}
						>
							{t("invites.copy")}
						</Button>
					</div>
				</div>
			)}

			<QueryTable
				query={invites}
				columns={columns}
				getRows={(data) => data}
				emptyMessage={t("invites.empty")}
				errorLabel={t("invites.failed_to_load")}
			/>
		</section>
	);
}
