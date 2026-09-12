// @vitest-environment jsdom

import { cleanup, render, screen } from "@testing-library/react";
import { createElement, type ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Role, StorageState } from "@/api/generated/trpc";

const state = vi.hoisted(() => ({
	status: undefined as { state: StorageState } | undefined,
	reason: "",
	detailsEnabled: [] as boolean[],
	live: 0,
}));

vi.mock("react-i18next", () => ({
	useTranslation: () => ({ t: (key: string) => key }),
}));
vi.mock("@/features/storage/queries", async (importOriginal) => {
	const actual =
		await importOriginal<typeof import("@/features/storage/queries")>();
	return {
		storageUnreadable: actual.storageUnreadable,
		useStorageStatus: () => ({ data: state.status }),
		useStorageDetails: (enabled: boolean) => {
			state.detailsEnabled.push(enabled);
			return { data: enabled ? { reason: state.reason } : undefined };
		},
		useLiveStorageStatus: () => {
			state.live++;
		},
		useAdoptStorage: () => ({ mutateAsync: vi.fn(), isPending: false }),
	};
});
vi.mock("@tanstack/react-router", () => ({
	Link: ({ children, to }: { children?: ReactNode; to: string }) =>
		createElement("a", { href: to }, children),
}));
vi.mock("@/integrations/tanstack-query/root-provider", () => ({
	trpcClient: {},
}));

import { clearUser, setUser } from "@/stores/auth";
import { StorageBanner } from "./StorageBanner";

function login(role: Role) {
	setUser({ id: "u", login: "u", displayName: "U", role });
}

afterEach(() => {
	cleanup();
	clearUser();
	state.status = undefined;
	state.reason = "";
	state.detailsEnabled = [];
	state.live = 0;
});

describe("StorageBanner", () => {
	it("renders nothing while storage is attached or unknown, but keeps the live feed", () => {
		login("viewer");
		render(createElement(StorageBanner));
		expect(screen.queryByTestId("storage-banner")).toBeNull();
		// A null payload (a mocked or degraded transport) must not crash the shell.
		state.status = null as unknown as undefined;
		render(createElement(StorageBanner));
		expect(screen.queryByTestId("storage-banner")).toBeNull();
		state.status = { state: "attached" };
		render(createElement(StorageBanner));
		expect(screen.queryByTestId("storage-banner")).toBeNull();
		expect(state.live).toBe(3);
	});

	it.each<StorageState>([
		"unattached",
		"unreachable",
	])("warns viewers that %s storage stops playback and recording, without owner facts", (storageState) => {
		login("viewer");
		state.status = { state: storageState };
		state.reason = "marker missing at /mnt/data";
		render(createElement(StorageBanner));
		const banner = screen.getByTestId("storage-banner");
		expect(banner.getAttribute("data-state")).toBe(storageState);
		expect(screen.getByText("storage.banner_unattached_title")).toBeTruthy();
		expect(screen.queryByText(/marker missing/)).toBeNull();
		expect(screen.queryByText("storage.banner_link")).toBeNull();
		expect(state.detailsEnabled.every((enabled) => enabled === false)).toBe(
			true,
		);
	});

	it("shows owners the reason and a link to the storage page", () => {
		login("owner");
		state.status = { state: "unattached" };
		state.reason = "storage unattached: marker missing";
		render(createElement(StorageBanner));
		expect(screen.getByText(/marker missing/)).toBeTruthy();
		const link = screen.getByText("storage.banner_link");
		expect(link.getAttribute("href")).toBe("/dashboard/system/storage");
		expect(state.detailsEnabled.at(-1)).toBe(true);
	});

	it("uses the milder read-only wording", () => {
		login("viewer");
		state.status = { state: "read_only" };
		render(createElement(StorageBanner));
		expect(screen.getByText("storage.banner_read_only_title")).toBeTruthy();
		expect(screen.queryByText("storage.banner_unattached_title")).toBeNull();
	});

	it("shows viewers that full storage can still play and be cleaned up", () => {
		login("viewer");
		state.status = { state: "full" };
		state.reason = "write /private/storage/probe: no space left on device";
		render(createElement(StorageBanner));
		expect(screen.getByText("storage.banner_full_title")).toBeTruthy();
		expect(screen.getByText("storage.banner_full_body")).toBeTruthy();
		expect(screen.queryByText("storage.banner_read_only_title")).toBeNull();
		expect(screen.queryByText(/private\/storage/)).toBeNull();
		expect(state.detailsEnabled.at(-1)).toBe(false);
	});
});
