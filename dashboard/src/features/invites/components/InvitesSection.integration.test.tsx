// @vitest-environment jsdom

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
	act,
	cleanup,
	fireEvent,
	render,
	screen,
	waitFor,
	within,
} from "@testing-library/react";
import { createTRPCClient, httpLink } from "@trpc/client";
import { afterEach, describe, expect, it, vi } from "vitest";
import { type AppRouter, TRPCProvider } from "@/api/trpc";
import type { InviteInfo } from "@/features/invites";
import { InvitesSection } from "./InvitesSection";

vi.mock("react-i18next", () => {
	const t = (key: string) => key;
	return { useTranslation: () => ({ t }) };
});
vi.mock("sonner", () => ({
	toast: { success: vi.fn(), error: vi.fn() },
}));

afterEach(cleanup);

const invite = {
	id: 7,
	role: "viewer",
	note: "original invitation",
	created_by: "admin",
	created_at: "2026-06-01T12:00:00Z",
	expires_at: "2099-06-02T12:00:00Z",
} satisfies InviteInfo;
const addedInvite = {
	...invite,
	id: 8,
	note: "new invitation",
} satisfies InviteInfo;
const issued = {
	id: invite.id,
	role: invite.role,
	expires_at: invite.expires_at,
	url: "https://example.test/invite/rotated-token",
};

function json(data: unknown) {
	return new Response(JSON.stringify({ result: { data } }), {
		headers: { "Content-Type": "application/json" },
	});
}

function rowFor(note: string) {
	return within(screen.getByRole("row", { name: new RegExp(note) }));
}

function expectActionsDisabled(note: string, disabled: boolean) {
	const row = rowFor(note);
	for (const name of ["invites.new_link", "invites.revoke"]) {
		expect(row.getByRole<HTMLButtonElement>("button", { name }).disabled).toBe(
			disabled,
		);
	}
}

describe("invitation action ownership", () => {
	it.each([
		"rotate",
		"revoke",
	] as const)("keeps %s attached to its invitation when the list changes", async (pendingAction) => {
		let releaseResponse!: () => void;
		const response = new Promise<void>((resolve) => {
			releaseResponse = resolve;
		});
		let rows = [invite];
		const calls: { action: string; id: number }[] = [];
		const queryClient = new QueryClient({
			defaultOptions: {
				queries: { retry: false, staleTime: Infinity, gcTime: 0 },
				mutations: { retry: false },
			},
		});
		const trpcClient = createTRPCClient<AppRouter>({
			links: [
				httpLink({
					url: "http://example.test/trpc",
					fetch: async (url, options) => {
						const procedure = new URL(String(url)).pathname;
						if (procedure === "/trpc/system.listInvites") return json(rows);
						const action =
							procedure === "/trpc/system.rotateInvite" ? "rotate" : "revoke";
						expect(procedure).toBe(`/trpc/system.${action}Invite`);
						const input = JSON.parse(String(options?.body));
						calls.push({ action, id: input.id });
						if (action === pendingAction) await response;
						if (action === "revoke") {
							rows = rows.filter((row) => row.id !== input.id);
							return json({ ok: true });
						}
						return json(issued);
					},
				}),
			],
		});
		const { unmount } = render(
			<QueryClientProvider client={queryClient}>
				<TRPCProvider trpcClient={trpcClient} queryClient={queryClient}>
					<InvitesSection />
				</TRPCProvider>
			</QueryClientProvider>,
		);
		try {
			await screen.findByText(invite.note);
			if (pendingAction === "revoke") {
				fireEvent.click(
					rowFor(invite.note).getByRole("button", { name: "invites.new_link" }),
				);
				await screen.findByText(issued.url);
				await waitFor(() => expectActionsDisabled(invite.note, false));
			}
			fireEvent.click(
				rowFor(invite.note).getByRole("button", {
					name:
						pendingAction === "rotate" ? "invites.new_link" : "invites.revoke",
				}),
			);
			await waitFor(() => expectActionsDisabled(invite.note, true));
			const startedCalls = [...calls];
			expect(startedCalls.at(-1)).toEqual({
				action: pendingAction,
				id: invite.id,
			});

			rows = [addedInvite, invite];
			await act(() => queryClient.invalidateQueries());
			await screen.findByText(addedInvite.note);
			expectActionsDisabled(invite.note, true);
			expectActionsDisabled(addedInvite.note, false);
			for (const name of ["invites.new_link", "invites.revoke"]) {
				fireEvent.click(rowFor(invite.note).getByRole("button", { name }));
			}
			expect(calls).toEqual(startedCalls);

			releaseResponse();
			if (pendingAction === "rotate") {
				await screen.findByText(issued.url);
				await waitFor(() => expectActionsDisabled(invite.note, false));
				fireEvent.click(
					rowFor(invite.note).getByRole("button", { name: "invites.revoke" }),
				);
			}
			await waitFor(() => {
				expect(screen.queryByText("invites.url_ready")).toBeNull();
				expect(screen.queryByText(invite.note)).toBeNull();
			});
			expectActionsDisabled(addedInvite.note, false);
			expect(calls).toEqual([
				{ action: "rotate", id: invite.id },
				{ action: "revoke", id: invite.id },
			]);
		} finally {
			releaseResponse();
			unmount();
			queryClient.clear();
		}
	});
});
