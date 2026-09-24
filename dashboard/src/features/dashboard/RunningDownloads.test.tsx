// @vitest-environment jsdom

import { cleanup, render, screen } from "@testing-library/react";
import { createElement, type ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type {
	ActiveDownloadResponse,
	VideoResponse,
} from "@/api/generated/trpc";
import { makeActiveDownload, makeVideo } from "@/test/fixtures";

// Mutable state the mocked useLiveActiveDownloads returns; each test sets it.
const live = vi.hoisted(() => ({
	data: undefined as ActiveDownloadResponse[] | undefined,
	dataUpdatedAt: 0,
	isLoading: false,
	isError: false,
	error: null as Error | null,
}));

vi.mock("react-i18next", () => ({
	useTranslation: () => ({
		t: (key: string, vars?: Record<string, unknown>) => {
			if (vars && "count" in vars) return `${key}:${vars.count}`;
			if (vars && "active" in vars) return `${key}:${vars.active}/${vars.max}`;
			return key;
		},
	}),
}));

// The data hooks are the seam under test; the per-row timeline poll and the
// live-clock are pinned so a row renders deterministically without a media axis.
vi.mock("@/features/videos", () => ({
	useLiveActiveDownloads: () => live,
	useDownloadCapacity: () => ({ data: { max_concurrent: 2 } }),
	useVideoTimeline: () => ({ data: undefined }),
	useCancelDownload: () => ({ mutate: vi.fn(), isPending: false }),
	useSetWatchLater: () => ({ mutate: vi.fn(), isPending: false }),
}));

vi.mock("@/hooks/useLiveSeconds", () => ({ useLiveSeconds: () => 0 }));

vi.mock("@tanstack/react-router", () => ({
	Link: ({ children }: { children?: ReactNode }) =>
		createElement("a", null, children),
}));

vi.mock("@/components/ui/avatar", () => ({
	Avatar: ({ name }: { name: string }) => createElement("span", null, name),
}));

import { RunningDownloads } from "./RunningDownloads";

function video(partial: Partial<VideoResponse> = {}): VideoResponse {
	return makeVideo(0, { status: "RUNNING", ...partial });
}

afterEach(() => {
	cleanup();
	live.data = undefined;
	live.dataUpdatedAt = 0;
	live.isLoading = false;
	live.isError = false;
	live.error = null;
});

describe("RunningDownloads", () => {
	it("shows the loading state until the first SSE sample arrives", () => {
		live.isLoading = true;
		render(createElement(RunningDownloads));
		expect(screen.getByText("common.loading")).toBeTruthy();
	});

	it("surfaces a subscription error with its message", () => {
		live.isError = true;
		live.error = new Error("stream down");
		render(createElement(RunningDownloads));
		expect(screen.getByText(/dashboard.running_now_failed/)).toBeTruthy();
		expect(screen.getByText(/stream down/)).toBeTruthy();
	});

	it("shows the empty state and a zero count when nothing is recording", () => {
		live.data = [];
		render(createElement(RunningDownloads));
		expect(screen.getByText("dashboard.running_now_empty")).toBeTruthy();
		expect(screen.getByText("dashboard.active_count:0")).toBeTruthy();
	});

	it("renders a row per active download, preferring broadcaster_name", () => {
		// Distinct display_name so the assertion proves broadcaster_name is the
		// rendered channel label, not the fallback.
		live.data = [
			makeActiveDownload({
				video: video({
					broadcaster_name: "Streamer",
					display_name: "fallback-name",
				}),
			}),
		];
		render(createElement(RunningDownloads));
		expect(screen.getAllByText("Streamer").length).toBeGreaterThan(0);
		expect(screen.queryByText("fallback-name")).toBeNull();
		expect(screen.getByLabelText("videos.watch_later.label")).toBeTruthy();
		expect(screen.getByText("dashboard.active_count:1")).toBeTruthy();
	});

	it("marks a running archive instead of showing the live dot", () => {
		live.data = [
			makeActiveDownload({
				video: video({ id: 1, job_id: "vod", source: "vod" }),
			}),
			makeActiveDownload({ video: video({ id: 2, job_id: "live" }) }),
		];
		render(createElement(RunningDownloads));
		expect(screen.getAllByTestId("running-archive-badge")).toHaveLength(1);
		expect(screen.getByText("videos.archive_badge")).toBeTruthy();
	});

	it("shows a percentage only when the run knows its size", () => {
		// A VOD archive reports its total from the first poll; a live
		// recording reports -1 until it ends.
		live.data = [
			makeActiveDownload({
				video: video({ job_id: "vod" }),
				percent: 42.4,
				eta: "3m",
			}),
			makeActiveDownload({
				video: video({ id: 2, job_id: "live" }),
				percent: -1,
			}),
		];
		render(createElement(RunningDownloads));
		expect(screen.getByText("42%")).toBeTruthy();
		expect(screen.getAllByText(/%$/).length).toBe(1);
		expect(screen.getByText("dashboard.eta_left")).toBeTruthy();
	});
});
