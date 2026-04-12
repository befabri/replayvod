import { VideoCamera } from "@phosphor-icons/react"
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
		? (errorMessages[error] ?? `Login failed: ${error}`)
		: null

	return (
		<div className="min-h-screen grid grid-cols-1 md:grid-cols-2">
			<section className="hidden md:flex flex-col justify-between p-10 bg-gradient-to-br from-primary/30 via-primary/10 to-background border-r border-border">
				<div className="flex items-center gap-2 text-lg font-heading font-semibold">
					<VideoCamera weight="fill" className="size-6 text-primary" />
					{t("app.name")}
				</div>
				<blockquote className="max-w-sm">
					<p className="text-lg italic text-foreground/90">
						Record, archive, and re-watch your favorite Twitch streams — on
						your own storage, on your own terms.
					</p>
				</blockquote>
			</section>

			<section className="flex items-center justify-center p-8">
				<div className="w-full max-w-sm space-y-6 text-center">
					<div className="md:hidden flex items-center justify-center gap-2 text-lg font-heading font-semibold">
						<VideoCamera weight="fill" className="size-6 text-primary" />
						{t("app.name")}
					</div>
					<div className="space-y-2">
						<h1 className="text-3xl font-heading font-bold">
							Welcome back
						</h1>
						<p className="text-sm text-muted-foreground">
							Sign in with your Twitch account to continue.
						</p>
					</div>

					{errorMessage && (
						<div
							role="alert"
							className="rounded-md bg-destructive/10 border border-destructive/20 p-3 text-destructive text-sm text-left"
						>
							{errorMessage}
						</div>
					)}

					<a
						href={`${API_URL}/api/v1/auth/twitch`}
						className="inline-flex w-full items-center justify-center gap-2 rounded-lg bg-primary px-6 py-3 text-primary-foreground font-medium hover:opacity-90 transition-opacity"
					>
						{t("auth.login")}
					</a>
				</div>
			</section>
		</div>
	)
}
