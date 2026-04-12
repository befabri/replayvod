import { createFileRoute } from "@tanstack/react-router"
import { useState } from "react"
import {
	useAddWhitelist,
	useRemoveWhitelist,
	useWhitelist,
} from "@/features/whitelist"

export const Route = createFileRoute("/dashboard/system/whitelist")({
	component: WhitelistPage,
})

function WhitelistPage() {
	const { data: entries, isLoading, error } = useWhitelist()
	const add = useAddWhitelist()
	const remove = useRemoveWhitelist()
	const [input, setInput] = useState("")

	const submit = (e: React.FormEvent) => {
		e.preventDefault()
		const v = input.trim()
		if (!v) return
		add.mutate(
			{ twitch_user_id: v },
			{ onSuccess: () => setInput("") },
		)
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

			{entries && entries.length === 0 && !isLoading && !error && (
				<div className="text-muted-foreground">No whitelist entries.</div>
			)}

			{entries && entries.length > 0 && (
				<div className="rounded-lg border border-border overflow-hidden">
					<table className="w-full text-sm">
						<thead className="bg-muted/50">
							<tr>
								<th className="text-left px-4 py-2 font-medium">
									Twitch User ID
								</th>
								<th className="text-left px-4 py-2 font-medium">Added</th>
								<th className="text-right px-4 py-2 font-medium">Actions</th>
							</tr>
						</thead>
						<tbody>
							{entries.map((e) => (
								<tr
									key={e.twitch_user_id}
									className="border-t border-border hover:bg-muted/30"
								>
									<td className="px-4 py-2 font-mono">{e.twitch_user_id}</td>
									<td className="px-4 py-2 text-muted-foreground">
										{new Date(e.added_at).toLocaleString()}
									</td>
									<td className="px-4 py-2 text-right">
										<button
											type="button"
											disabled={remove.isPending}
											onClick={() =>
												remove.mutate({ twitch_user_id: e.twitch_user_id })
											}
											className="text-destructive hover:underline disabled:opacity-60"
										>
											Remove
										</button>
									</td>
								</tr>
							))}
						</tbody>
					</table>
				</div>
			)}
		</div>
	)
}
