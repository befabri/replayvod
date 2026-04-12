import { createFileRoute } from "@tanstack/react-router"
import { useStore } from "@tanstack/react-store"
import { useMemo } from "react"
import { DataTable } from "@/components/ui/data-table"
import { useUsers } from "@/features/users"
import { userColumns } from "@/features/users/components/columns"
import { authStore } from "@/stores/auth"

export const Route = createFileRoute("/dashboard/system/users")({
	component: UsersPage,
})

function UsersPage() {
	const currentUser = useStore(authStore, (s) => s.user)
	const { data: users, isLoading, error } = useUsers()

	const columns = useMemo(() => userColumns(currentUser?.id), [currentUser?.id])

	return (
		<div className="p-8">
			<h1 className="text-3xl font-heading font-bold mb-6">Users</h1>

			{isLoading && <div className="text-muted-foreground">Loading…</div>}

			{error && (
				<div className="rounded-md bg-destructive/10 border border-destructive/20 p-4 text-destructive text-sm">
					Failed to load users: {error.message}
				</div>
			)}

			{users && (
				<DataTable
					columns={columns}
					data={users}
					emptyMessage="No users."
				/>
			)}
		</div>
	)
}
