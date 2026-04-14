import { useForm } from "@tanstack/react-form";
import { createFileRoute } from "@tanstack/react-router";
import { z } from "zod";
import { TitledLayout } from "@/components/layout/titled-layout";
import { Button } from "@/components/ui/button";
import { DataTable } from "@/components/ui/data-table";
import { Input } from "@/components/ui/input";
import { useAddWhitelist, useWhitelist } from "@/features/whitelist";
import { whitelistColumns } from "@/features/whitelist/components/columns";

// WhitelistAddSchema narrows the server's WhitelistIDInput (generated
// schema is untyped on content). Twitch numeric IDs only — rejects
// blanks and non-digit input at the client boundary so the server's
// validator isn't the first line of defense.
const WhitelistAddSchema = z.object({
	twitch_user_id: z
		.string()
		.trim()
		.min(1, "Required")
		.regex(/^\d+$/, "Must be a numeric Twitch user ID"),
});

type WhitelistAddValues = z.infer<typeof WhitelistAddSchema>;

export const Route = createFileRoute("/dashboard/system/whitelist")({
	component: WhitelistPage,
});

function WhitelistPage() {
	const { data: entries, isLoading, error } = useWhitelist();
	const add = useAddWhitelist();

	const form = useForm({
		defaultValues: { twitch_user_id: "" } as WhitelistAddValues,
		validators: { onSubmit: WhitelistAddSchema },
		onSubmit: async ({ value, formApi }) => {
			await add.mutateAsync({ twitch_user_id: value.twitch_user_id.trim() });
			formApi.reset();
		},
	});

	return (
		<TitledLayout title="Whitelist">
			<div className="max-w-2xl">
				<p className="text-muted-foreground mb-6 -mt-6">
					When whitelist is enabled in the server config, only Twitch user IDs
					listed here can sign in.
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
								placeholder="Twitch user ID (numeric)"
								aria-invalid={
									field.state.meta.errors.length > 0 ? true : undefined
								}
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
								{isSubmitting || add.isPending ? "Adding…" : "Add"}
							</Button>
						)}
					</form.Subscribe>
				</form>

				{add.isError && (
					<div className="mb-4 rounded-md bg-destructive/10 border border-destructive/20 p-3 text-destructive text-sm">
						{add.error?.message ?? "Failed to add"}
					</div>
				)}

				{isLoading && <div className="text-muted-foreground">Loading…</div>}
				{error && (
					<div className="rounded-md bg-destructive/10 border border-destructive/20 p-4 text-destructive text-sm">
						Failed to load whitelist: {error.message}
					</div>
				)}

				{entries && (
					<DataTable
						columns={whitelistColumns}
						data={entries}
						emptyMessage="No whitelist entries."
					/>
				)}
			</div>
		</TitledLayout>
	);
}
