import type { Role, UserInfo } from "@/api/generated/trpc";

export interface FakeUser {
	id: string;
	login: string;
	displayName: string;
	email: string;
}

export const CURRENT_USER: FakeUser = {
	id: "u1",
	login: "alice",
	displayName: "Alice",
	email: "alice@example.com",
};

export const USERS: readonly FakeUser[] = [
	CURRENT_USER,
	{ id: "u2", login: "bob", displayName: "Bob", email: "bob@example.com" },
	{
		id: "u3",
		login: "charlie",
		displayName: "Charlie",
		email: "charlie@example.com",
	},
	{ id: "u4", login: "dana", displayName: "Dana", email: "dana@example.com" },
];

const LISTED_ROLES: Record<string, Role> = {
	u2: "viewer",
	u3: "owner",
	u4: "admin",
};

export function makeUserInfos(currentUserRole: Role): UserInfo[] {
	return USERS.map((user, index) => ({
		id: user.id,
		login: user.login,
		display_name: user.displayName,
		email: user.email,
		role: user.id === CURRENT_USER.id ? currentUserRole : LISTED_ROLES[user.id],
		created_at: new Date(Date.UTC(2026, 0, 1 + index * 9)).toISOString(),
		updated_at: new Date(Date.UTC(2026, 0, 1 + index * 9)).toISOString(),
	}));
}

export function userAt(index: number): FakeUser {
	return USERS[index % USERS.length];
}
