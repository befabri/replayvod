import { Link, createFileRoute } from "@tanstack/react-router"
import { useTranslation } from "react-i18next"
import { useMyRequests } from "@/features/requests"

export const Route = createFileRoute("/dashboard/requests")({
	component: RequestsPage,
})

function RequestsPage() {
	const { t } = useTranslation()
	const { data, isLoading, error } = useMyRequests()

	return (
		<div className="p-8">
			<h1 className="text-3xl font-heading font-bold mb-6">
				{t("nav.requests")}
			</h1>

			{isLoading && <div className="text-muted-foreground">Loading…</div>}

			{error && (
				<div className="rounded-md bg-destructive/10 border border-destructive/20 p-4 text-destructive text-sm">
					{error.message}
				</div>
			)}

			{data && data.length === 0 && !isLoading && !error && (
				<div className="text-muted-foreground">
					No requests yet. Request a download by clicking "Request" on any
					video detail page.
				</div>
			)}

			{data && data.length > 0 && (
				<div className="rounded-lg border border-border overflow-hidden">
					<table className="w-full text-sm">
						<thead className="bg-muted/50">
							<tr>
								<th className="text-left px-4 py-2 font-medium">Title</th>
								<th className="text-left px-4 py-2 font-medium">Status</th>
								<th className="text-right px-4 py-2 font-medium">Actions</th>
							</tr>
						</thead>
						<tbody>
							{data.map((v) => (
								<tr
									key={v.id}
									className="border-t border-border hover:bg-muted/30"
								>
									<td className="px-4 py-2">{v.display_name}</td>
									<td className="px-4 py-2">
										{t(`videos.status.${v.status}` as const, v.status)}
									</td>
									<td className="px-4 py-2 text-right">
										{v.status === "DONE" && (
											<Link
												// biome-ignore lint/suspicious/noExplicitAny: param route typing
												to={"/dashboard/watch/$videoId" as any}
												params={{ videoId: String(v.id) } as any}
												className="text-primary hover:underline"
											>
												Watch
											</Link>
										)}
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
