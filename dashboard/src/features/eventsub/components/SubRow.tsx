import { useTranslation } from "react-i18next"
import { useUnsubscribe } from "@/features/eventsub"

// Row sub shape as the query actually returns it; condition is serialized
// as json.RawMessage server-side so TRPC infers it as potentially absent.
export type SubRowData = {
	id: string
	type: string
	version: string
	status: string
	cost: number
	broadcaster_id?: string
}

export function SubRow({ sub }: { sub: SubRowData }) {
	const { t } = useTranslation()
	const unsub = useUnsubscribe()
	return (
		<tr className="border-t border-border">
			<td className="px-3 py-2 font-mono text-xs">
				{sub.type}{" "}
				<span className="text-muted-foreground">v{sub.version}</span>
			</td>
			<td className="px-3 py-2 font-mono text-xs">
				{sub.broadcaster_id ?? "—"}
			</td>
			<td className="px-3 py-2">{sub.status}</td>
			<td className="px-3 py-2 text-right">{sub.cost}</td>
			<td className="px-3 py-2 text-right">
				<button
					type="button"
					onClick={() => unsub.mutate({ id: sub.id, reason: "manual" })}
					disabled={unsub.isPending}
					className="text-destructive hover:underline text-xs disabled:opacity-60"
				>
					{t("eventsub.unsubscribe")}
				</button>
			</td>
		</tr>
	)
}
