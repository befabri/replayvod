import { createFileRoute } from "@tanstack/react-router"
import { useTranslation } from "react-i18next"
import { API_URL } from "@/env"

export const Route = createFileRoute("/login")({
	validateSearch: (search: Record<string, unknown>) => ({
		error: typeof search.error === "string" ? search.error : undefined,
	}),
	component: LoginPage,
})

const errorMessages: Record<string, string> = {
	not_whitelisted: "Your Twitch account is not authorized to use this app.",
	access_denied: "You declined the authorization. Please try again.",
	invalid_state: "Login session expired. Please try again.",
	invalid_pkce: "Login session expired. Please try again.",
}

function LoginPage() {
	const { t } = useTranslation()
	const { error } = Route.useSearch()

	const errorMessage = error
		? errorMessages[error] || `Login failed: ${error}`
		: null

	return (
		<div className="flex min-h-screen items-center justify-center">
			<div className="text-center space-y-6">
				<h1 className="text-4xl font-bold">{t("app.name")}</h1>
				{errorMessage && (
					<div className="rounded-md bg-destructive/10 border border-destructive/20 p-4 text-destructive text-sm max-w-md">
						{errorMessage}
					</div>
				)}
				<a
					href={`${API_URL}/api/v1/auth/twitch`}
					className="inline-flex items-center gap-2 rounded-lg bg-primary px-6 py-3 text-primary-foreground font-medium hover:opacity-90 transition-opacity"
				>
					{t("auth.login")}
				</a>
			</div>
		</div>
	)
}
