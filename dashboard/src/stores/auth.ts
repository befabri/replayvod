import { Store, Derived } from "@tanstack/store"

interface AuthUser {
  id: string
  login: string
  displayName: string
  profileImageUrl?: string
  role: string
}

interface AuthState {
  isAuthenticated: boolean
  user: AuthUser | null
  isLoading: boolean
}

function getInitialState(): AuthState {
  return {
    isAuthenticated: false,
    user: null,
    isLoading: true,
  }
}

export const authStore = new Store<AuthState>(getInitialState())

export const isAuthenticated = new Derived({
  fn: () => authStore.state.isAuthenticated && !!authStore.state.user,
  deps: [authStore],
})

export function setUser(user: AuthUser) {
  authStore.setState((s) => ({
    ...s,
    isAuthenticated: true,
    user,
    isLoading: false,
  }))
}

export function logout() {
  authStore.setState(() => ({
    isAuthenticated: false,
    user: null,
    isLoading: false,
  }))
}

export function setLoading(isLoading: boolean) {
  authStore.setState((s) => ({ ...s, isLoading }))
}
