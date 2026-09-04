import { useForm } from "@tanstack/react-form";
import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { QueryTable } from "@/components/ui/query-table";
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

const SELECT_CLASS =
	"rounded-md border border-border bg-background px-2 py-1.5 text-sm";

// InvitesSection is the invite management block on the users page:
// create form, copy-once URL panel, and the invites table.
export function InvitesSection() {
	const { t } = useTranslation();
	const invites = useInvites();
	const create = useCreateInvite();
	const columns = useMemo(() => inviteColumns(t), [t]);

	const ttlLabels: Record<(typeof TTL_MINUTES)[number], string> = {
		"60": t("invites.ttl_1h"),
		"1440": t("invites.ttl_24h"),
		"10080": t("invites.ttl_7d"),
		"43200": t("invites.ttl_30d"),
	};

	const form = useForm({
		defaultValues: {
			role: "viewer",
			ttl_minutes: "1440",
			note: "",
		} as InviteFormValues,
		onSubmit: async ({ value }) => {
			const note = value.note.trim();
			try {
				await create.mutateAsync({
					role: value.role,
					ttl_minutes: Number(value.ttl_minutes),
					note: note === "" ? undefined : note,
				});
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
		<section className="mt-10 max-w-3xl">
			<h2 className="text-xl font-medium mb-2">{t("invites.title")}</h2>
			<p className="text-muted-foreground text-sm mb-4">
				{t("invites.description")}
			</p>

			<form
				onSubmit={(e) => {
					e.preventDefault();
					e.stopPropagation();
					void form.handleSubmit();
				}}
				className="flex flex-wrap items-center gap-2 mb-4"
			>
				<form.Field name="role">
					{(field) => (
						<select
							value={field.state.value}
							onChange={(e) =>
								field.handleChange(e.target.value as InviteFormValues["role"])
							}
							aria-label={t("invites.col_role")}
							className={SELECT_CLASS}
						>
							<option value="viewer">{t("users.role_viewer")}</option>
							<option value="admin">{t("users.role_admin")}</option>
						</select>
					)}
				</form.Field>
				<form.Field name="ttl_minutes">
					{(field) => (
						<select
							value={field.state.value}
							onChange={(e) => field.handleChange(e.target.value)}
							aria-label={t("invites.col_expires")}
							className={SELECT_CLASS}
						>
							{TTL_MINUTES.map((minutes) => (
								<option key={minutes} value={minutes}>
									{ttlLabels[minutes]}
								</option>
							))}
						</select>
					)}
				</form.Field>
				<form.Field name="note">
					{(field) => (
						<Input
							type="text"
							value={field.state.value}
							onChange={(e) => field.handleChange(e.target.value)}
							maxLength={200}
							placeholder={t("invites.note_placeholder")}
							className="flex-1 min-w-48"
						/>
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
				<div className="mb-4 rounded-md bg-destructive/10 border border-destructive/20 p-3 text-destructive text-sm">
					{create.error?.message ?? t("invites.failed_to_create")}
				</div>
			)}

			{create.data && (
				<div className="mb-6 rounded-md border border-border bg-card p-3">
					<p className="text-sm mb-2">{t("invites.url_ready")}</p>
					<div className="flex items-center gap-2">
						<code className="flex-1 truncate rounded-md bg-background border border-border px-2 py-1.5 text-sm">
							{create.data.url}
						</code>
						<Button
							type="button"
							variant="outline"
							onClick={() => void copyUrl(create.data.url)}
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
