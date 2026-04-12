import { createFileRoute } from "@tanstack/react-router"
import { useTranslation } from "react-i18next"
import { API_URL } from "@/env"

export const Route = createFileRoute("/login")({
  component: LoginPage,
})

function LoginPage() {
  const { t } = useTranslation()

  return (
    <div className="flex min-h-screen items-center justify-center">
      <div className="text-center space-y-6">
        <h1 className="text-4xl font-bold">{t("app.name")}</h1>
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
