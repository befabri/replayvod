import { useEffect } from "react"
import { useNavigate } from "@tanstack/react-router"
import { useStore } from "@tanstack/react-store"
import { authStore } from "@/stores/auth"

export function useRequireAuth() {
  const isAuthenticated = useStore(authStore, (s) => s.isAuthenticated)
  const navigate = useNavigate()

  useEffect(() => {
    if (!isAuthenticated) {
      navigate({ to: "/login" })
    }
  }, [isAuthenticated, navigate])

  return { isAuthenticated }
}
