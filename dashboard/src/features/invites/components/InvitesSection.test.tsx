// @vitest-environment jsdom

import {
	cleanup,
	fireEvent,
	render,
	screen,
	waitFor,
} from "@testing-library/react";
import { createElement } from "react";
import { toast } from "sonner";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("react-i18next", () => ({
	useTranslation: () => ({ t: (key: string) => key }),
}));
vi.mock("sonner", () => ({
	toast: { success: vi.fn(), error: vi.fn() },
}));
// The wire submission and the copy-once panel are under test; the
// invites table is not.
vi.mock("@/components/ui/query-table", () => ({
	QueryTable: () => null,
}));
const create = {
	mutateAsync: vi.fn(async () => ({})),
	isPending: false,
	isError: false,
	error: null as Error | null,
	data: undefined as { url: string } | undefined,
};
vi.mock("@/features/invites", () => ({
	useInvites: () => ({ data: [], isLoading: false, isError: false }),
	useCreateInvite: () => create,
}));

import { InvitesSection } from "./InvitesSection";

beforeEach(() => {
	create.mutateAsync.mockClear();
	create.data = undefined;
	create.isError = false;
	create.error = null;
	vi.clearAllMocks();
});
afterEach(() => {
	cleanup();
	vi.unstubAllGlobals();
});

function fillAndSubmit({ role, note }: { role?: string; note?: string }) {
	render(createElement(InvitesSection));
	if (role) {
		fireEvent.change(screen.getByLabelText("invites.col_role"), {
			target: { value: role },
		});
	}
	fireEvent.change(screen.getByLabelText("invites.col_expires"), {
		target: { value: "60" },
	});
	if (note !== undefined) {
		fireEvent.change(screen.getByPlaceholderText("invites.note_placeholder"), {
			target: { value: note },
		});
	}
	fireEvent.click(screen.getByRole("button", { name: "invites.create" }));
}

describe("InvitesSection create flow", () => {
	it("submits ttl_minutes as a number and the note trimmed", async () => {
		fillAndSubmit({ role: "admin", note: "  for bob  " });
		await waitFor(() => {
			expect(create.mutateAsync).toHaveBeenCalledWith({
				role: "admin",
				ttl_minutes: 60,
				note: "for bob",
			});
		});
	});

	it("drops an empty note instead of sending it", async () => {
		fillAndSubmit({ note: "   " });
		await waitFor(() => {
			expect(create.mutateAsync).toHaveBeenCalledWith({
				role: "viewer",
				ttl_minutes: 60,
				note: undefined,
			});
		});
	});

	it("shows the copy-once URL panel when the invite was created", () => {
		create.data = { url: "https://dash.example/invite/raw-token" };
		render(createElement(InvitesSection));
		expect(screen.getByText("invites.url_ready")).toBeTruthy();
		expect(
			screen.getByText("https://dash.example/invite/raw-token"),
		).toBeTruthy();
		expect(screen.getByRole("button", { name: "invites.copy" })).toBeTruthy();
	});

	it("keeps the form usable after a rejected create and lets the admin retry", async () => {
		create.mutateAsync.mockRejectedValueOnce(new Error("create unavailable"));
		fillAndSubmit({ role: "admin", note: "for bob" });
		await waitFor(() => {
			expect(create.mutateAsync).toHaveBeenCalledTimes(1);
			expect(
				screen.getByRole<HTMLButtonElement>("button", {
					name: "invites.create",
				}).disabled,
			).toBe(false);
		});
		expect(
			screen.getByPlaceholderText<HTMLInputElement>("invites.note_placeholder")
				.value,
		).toBe("for bob");
		expect(screen.queryByText("invites.url_ready")).toBeNull();
		fireEvent.click(screen.getByRole("button", { name: "invites.create" }));
		await waitFor(() => expect(create.mutateAsync).toHaveBeenCalledTimes(2));
		expect(create.mutateAsync).toHaveBeenLastCalledWith({
			role: "admin",
			ttl_minutes: 60,
			note: "for bob",
		});
	});

	it.each([
		false,
		true,
	])("reports clipboard failure=%s when copying the returned URL", async (fail) => {
		const url = "https://dash.example/invite/raw-token";
		const writeText = vi.fn(async () => {
			if (fail) throw new Error("clipboard unavailable");
		});
		vi.stubGlobal("navigator", { clipboard: { writeText } });
		create.data = { url };
		render(createElement(InvitesSection));
		fireEvent.click(screen.getByRole("button", { name: "invites.copy" }));
		await waitFor(() => {
			expect(writeText).toHaveBeenCalledWith(url);
			expect(fail ? toast.error : toast.success).toHaveBeenCalledWith(
				fail ? "invites.url_copy_failed" : "invites.url_copied",
			);
		});
		expect(screen.getByText(url)).toBeTruthy();
	});
});
