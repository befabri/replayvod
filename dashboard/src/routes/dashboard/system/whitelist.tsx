import { createFileRoute } from "@tanstack/react-router"
import { useState } from "react"
import { DataTable } from "@/components/ui/data-table"
import { useAddWhitelist, useWhitelist } from "@/features/whitelist"
import { whitelistColumns } from "@/features/whitelist/components/columns"

export const Route = createFileRoute("/dashboard/system/whitelist")({
	component: WhitelistPage,
})

function WhitelistPage() {
	const { data: entries, isLoading, error } = useWhitelist()
	const add = useAddWhitelist()
	const [input, setInput] = useState("")

	const submit = (e: React.FormEvent) => {
		e.preventDefault()
		const v = input.trim()
		if (!v) return
		add.mutate({ twitch_user_id: v }, { onSuccess: () => setInput("") })
	}

	return (
		<div className="p-8 max-w-2xl">
			<h1 className="text-3xl font-heading font-bold mb-2">Whitelist</h1>
			<p className="text-sm text-muted-foreground mb-6">
				When whitelist is enabled in the server config, only Twitch user IDs
				listed here can sign in.
			</p>

			<form onSubmit={submit} className="flex gap-2 mb-6">
				<input
					type="text"
					value={input}
					onChange={(e) => setInput(e.target.value)}
					placeholder="Twitch user ID (numeric)"
					className="flex-1 rounded-md border border-border bg-background px-3 py-2 text-sm"
				/>
				<button
					type="submit"
					disabled={add.isPending || !input.trim()}
					className="rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground disabled:opacity-60"
				>
					{add.isPending ? "Adding…" : "Add"}
				</button>
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
	)
}
