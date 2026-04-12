import { createFileRoute, useNavigate } from "@tanstack/react-router"
import { useEffect } from "react"
import { useStore } from "@tanstack/react-store"
import { authStore } from "@/stores/auth"

export const Route = createFileRoute("/")({
	component: IndexPage,
})

function IndexPage() {
	const state = useStore(authStore, (s) => s)
	const navigate = useNavigate()

	useEffect(() => {
		if (state.isLoading) return
		if (state.isAuthenticated) {
			navigate({ to: "/dashboard" })
		} else {
			navigate({ to: "/login", search: { error: undefined } })
		}
	}, [state, navigate])

	return null
}
