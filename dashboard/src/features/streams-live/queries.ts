import { useRef, useState } from "react"
import { useSubscription } from "@trpc/tanstack-react-query"
import type { StreamLiveEvent } from "@/api/generated/trpc"
import { useTRPC } from "@/api/trpc"

// useLiveStreams keeps a rolling buffer of the most recent
// stream.live events in React state. Subscribers render a toast /
// banner / card from this; we don't route these through TanStack
// Query because there's no paginated "history" endpoint to keep
// coherent — the SSE feed IS the source of truth for live channels.
export function useLiveStreams(max = 5) {
	const trpc = useTRPC()
	const [events, setEvents] = useState<StreamLiveEvent[]>([])
	const maxRef = useRef(max)
	maxRef.current = max

	useSubscription({
		...trpc.stream.live.subscriptionOptions(),
		onData: (event) => {
			setEvents((prev) => {
				const next = [event, ...prev]
				return next.length > maxRef.current
					? next.slice(0, maxRef.current)
					: next
			})
		},
	})

	return events
}
