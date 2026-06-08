import { useForm } from "@tanstack/react-form";
import { createFileRoute } from "@tanstack/react-router";
import type { TFunction } from "i18next";
import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { z } from "zod";
import { TitledLayout } from "@/components/layout/titled-layout";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { QueryTable } from "@/components/ui/query-table";
import { useAddWhitelist, useWhitelist } from "@/features/whitelist";
import { whitelistColumns } from "@/features/whitelist/components/columns";

// WhitelistAddSchema narrows the server's WhitelistIDInput (generated
// schema is untyped on content). Twitch numeric IDs only — rejects
// blanks and non-digit input at the client boundary so the server's
// validator isn't the first line of defense.
function whitelistAddSchema(t: TFunction) {
	return z.object({
		twitch_user_id: z
			.string()
			.trim()
			.min(1, t("whitelist.validation_required"))
			.regex(/^\d+$/, t("whitelist.validation_numeric")),
	});
}

type WhitelistAddValues = {
	twitch_user_id: string;
};

export const Route = createFileRoute("/dashboard/system/whitelist")({
	component: WhitelistPage,
});

function WhitelistPage() {
	const { t } = useTranslation();
	const entries = useWhitelist();
	const add = useAddWhitelist();
	const schema = useMemo(() => whitelistAddSchema(t), [t]);
	const columns = useMemo(() => whitelistColumns(t), [t]);

	const form = useForm({
		defaultValues: { twitch_user_id: "" } satisfies WhitelistAddValues,
		validators: { onSubmit: schema },
		onSubmit: async ({ value, formApi }) => {
			await add.mutateAsync({ twitch_user_id: value.twitch_user_id.trim() });
			formApi.reset();
		},
	});

	return (
		<TitledLayout title={t("whitelist.title")}>
			<div className="max-w-2xl">
				<p className="text-muted-foreground mb-6 -mt-6">
					{t("whitelist.description")}
				</p>

				<form
					onSubmit={(e) => {
						e.preventDefault();
						e.stopPropagation();
						void form.handleSubmit();
					}}
					className="flex gap-2 mb-6"
				>
					<form.Field name="twitch_user_id">
						{(field) => (
							<Input
								type="text"
								value={field.state.value}
								onChange={(e) => field.handleChange(e.target.value)}
								onBlur={field.handleBlur}
								aria-invalid={
									field.state.meta.errors.length > 0 ? true : undefined
								}
								placeholder={t("whitelist.twitch_user_id_placeholder")}
								className="flex-1"
							/>
						)}
					</form.Field>
					<form.Subscribe
						selector={(s) => [s.canSubmit, s.isSubmitting] as const}
					>
						{([canSubmit, isSubmitting]) => (
							<Button
								type="submit"
								disabled={!canSubmit || isSubmitting || add.isPending}
							>
								{isSubmitting || add.isPending
									? t("whitelist.adding")
									: t("whitelist.add")}
							</Button>
						)}
					</form.Subscribe>
				</form>

				{add.isError && (
					<div className="mb-4 rounded-md bg-destructive/10 border border-destructive/20 p-3 text-destructive text-sm">
						{add.error?.message ?? t("whitelist.failed_to_add")}
					</div>
				)}

				<QueryTable
					query={entries}
					columns={columns}
					getRows={(data) => data}
					emptyMessage={t("whitelist.empty")}
					errorLabel={t("whitelist.failed_to_load")}
				/>
			</div>
		</TitledLayout>
	);
}
