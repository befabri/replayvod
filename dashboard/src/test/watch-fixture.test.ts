import type { Page } from "@playwright/test";
import { beforeEach, expect, it, vi } from "vitest";
import { mockTrpc, SESSION, trpcOk } from "../../tests/support/trpc";
import {
	mockWatchPage,
	userState,
	videoRecording,
} from "../../tests/support/watch";
import { USER_SETTINGS } from "./playback-settings";

vi.mock("../../tests/support/trpc", async (importOriginal) => ({
	...(await importOriginal<typeof import("../../tests/support/trpc")>()),
	mockTrpc: vi.fn(),
}));

beforeEach(() => vi.clearAllMocks());

it.each([
	["auth.session", "video.relatedRecordings", "video.getById", "settings.get"],
	["video.getById", "settings.get", "video.relatedRecordings", "auth.session"],
])("keeps recording data when session refresh shares the watch query batch: %j", async (...procedures) => {
	const current = videoRecording(78, 30, { user_state: userState(12) });
	await mockWatchPage({} as Page, { video: () => current });
	const resolve = vi.mocked(mockTrpc).mock.lastCall?.[1];
	const result = resolve?.(
		procedures,
		`http://example.test/trpc/${procedures.join(",")}?batch=1`,
	);
	const expected: Record<string, unknown> = {
		"auth.session": SESSION,
		"settings.get": USER_SETTINGS,
		"video.getById": current,
		"video.relatedRecordings": { items: [] },
	};
	expect(result).toEqual({
		status: 200,
		body: trpcOk(procedures.map((procedure) => expected[procedure])),
	});
});
