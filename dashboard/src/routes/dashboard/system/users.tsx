import { createFileRoute } from "@tanstack/react-router"
import { useStore } from "@tanstack/react-store"
import { useUsers, useUpdateUserRole } from "@/features/users"
import { authStore, type Role } from "@/stores/auth"

export const Route = createFileRoute("/dashboard/system/users")({
	component: UsersPage,
})

function UsersPage() {
	const currentUser = useStore(authStore, (s) => s.user)
	const { data: users, isLoading, error } = useUsers()
	const update = useUpdateUserRole()

	return (
		<div className="p-8">
			<h1 className="text-3xl font-heading font-bold mb-6">Users</h1>

			{isLoading && <div className="text-muted-foreground">Loading…</div>}

			{error && (
				<div className="rounded-md bg-destructive/10 border border-destructive/20 p-4 text-destructive text-sm">
					Failed to load users: {error.message}
				</div>
			)}

			{users && users.length > 0 && (
				<div className="rounded-lg border border-border overflow-hidden">
					<table className="w-full text-sm">
						<thead className="bg-muted/50">
							<tr>
								<th className="text-left px-4 py-2 font-medium">User</th>
								<th className="text-left px-4 py-2 font-medium">Login</th>
								<th className="text-left px-4 py-2 font-medium">Role</th>
								<th className="text-left px-4 py-2 font-medium">Joined</th>
							</tr>
						</thead>
						<tbody>
							{users.map((u) => {
								const isSelf = u.id === currentUser?.id
								return (
									<tr
										key={u.id}
										className="border-t border-border hover:bg-muted/30"
									>
										<td className="px-4 py-2 flex items-center gap-2">
											{u.profile_image_url && (
												<img
													src={u.profile_image_url}
													alt=""
													className="w-8 h-8 rounded-full"
												/>
											)}
											<span>{u.display_name}</span>
											{isSelf && (
												<span className="text-xs text-muted-foreground">
													(you)
												</span>
											)}
										</td>
										<td className="px-4 py-2 text-muted-foreground">
											@{u.login}
										</td>
										<td className="px-4 py-2">
											<select
												value={u.role}
												disabled={isSelf}
												onChange={(e) =>
													update.mutate({
														user_id: u.id,
														role: e.target.value as Role,
													})
												}
												className="rounded-md border border-border bg-background px-2 py-1 text-sm disabled:opacity-60"
											>
												<option value="viewer">viewer</option>
												<option value="admin">admin</option>
												<option value="owner">owner</option>
											</select>
										</td>
										<td className="px-4 py-2 text-muted-foreground">
											{new Date(u.created_at).toLocaleDateString()}
										</td>
									</tr>
								)
							})}
						</tbody>
					</table>
				</div>
			)}

			{update.isError && (
				<div className="mt-4 rounded-md bg-destructive/10 border border-destructive/20 p-3 text-destructive text-sm">
					{update.error?.message ?? "Failed to update role"}
				</div>
			)}
		</div>
	)
}
