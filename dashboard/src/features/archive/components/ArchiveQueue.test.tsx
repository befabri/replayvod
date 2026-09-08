// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { createElement, type ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { ArchiveQueueResponse, VideoResponse } from "@/api/generated/trpc";

const state = vi.hoisted(() => ({
	queue: {
		data: undefined as ArchiveQueueResponse | undefined,
		isLoading: false,
		isError: false,
		error: null as Error | null,
	},
	canManage: true,
	cancel: { mutate: vi.fn(), isPending: false },
	dequeue: { mutate: vi.fn(), isPending: false },
	retry: { mutate: vi.fn(), isPending: false },
	cancelRetry: { mutate: vi.fn(), isPending: false },
	active: { data: undefined as unknown[] | undefined },
}));

vi.mock("react-i18next", () => ({
	useTranslation: () => ({
		t: (key: string, vars?: Record<string, unknown>) =>
			typeof vars === "object" && vars !== null && "count" in vars
				? `${key}:${vars.count}`
				: key,
		i18n: { language: "en" },
	}),
}));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("@/features/archive/queries", () => ({
	useArchiveQueue: () => state.queue,
	useDequeueArchive: () => state.dequeue,
	useRetryArchive: () => state.retry,
	useCancelArchiveRetry: () => state.cancelRetry,
	useLiveArchiveQueue: () => {},
}));
vi.mock("@/features/videos/queries", () => ({
	useCancelDownload: () => state.cancel,
	useDownloadCapacity: () => ({
		data: { max_concurrent: 2, archive_max_concurrent: 1 },
	}),
	useLiveActiveDownloads: () => state.active,
}));
vi.mock("@/features/videos/permissions", () => ({
	useCanManageVideos: () => state.canManage,
}));
vi.mock("@tanstack/react-router", () => ({
	Link: ({ children }: { children?: ReactNode }) =>
		createElement("a", null, children),
}));

import { ArchiveQueue } from "./ArchiveQueue";

function video(partial: Partial<VideoResponse>): VideoResponse {
	return {
		id: 1,
		job_id: "job-1",
		filename: "f",
		display_name: "Streamer",
		broadcaster_name: "Streamer",
		title: "A stream",
		status: "PENDING",
		completion_kind: "complete",
		truncated: false,
		quality: "HIGH",
		is_audio_only: false,
		broadcaster_id: "b1",
		viewer_count: 0,
		language: "en",
		start_download_at: "2026-09-01T00:00:00Z",
		source: "vod",
		twitch_video_id: "100",
		broadcast_at: "2026-08-20T12:00:00Z",
		...partial,
	};
}

afterEach(() => {
	cleanup();
	vi.clearAllMocks();
	state.canManage = true;
});

describe("ArchiveQueue", () => {
	it("shows the empty state and the archive slot count", () => {
		state.queue.data = { queue: [], failures: [] };
		render(createElement(ArchiveQueue));
		expect(screen.getByText("archive.queue_empty")).toBeTruthy();
		expect(screen.getByText("archive.queue_capacity:1")).toBeTruthy();
	});

	it("offers remove for queued rows and cancel for running ones", () => {
		state.queue.data = {
			queue: [
				video({ id: 1, job_id: "job-1", status: "RUNNING" }),
				video({ id: 2, job_id: "job-2", status: "PENDING", title: "Second" }),
			],
			failures: [],
		};
		render(createElement(ArchiveQueue));
		fireEvent.click(screen.getByRole("button", { name: "archive.cancel" }));
		expect(state.cancel.mutate).toHaveBeenCalledWith({ job_id: "job-1" });
		fireEvent.click(screen.getByRole("button", { name: "archive.remove" }));
		expect(state.dequeue.mutate).toHaveBeenCalledWith(
			{ video_id: 2 },
			expect.anything(),
		);
	});

	it("shows live percent for a running archive and a waiting label for queued ones", () => {
		state.queue.data = {
			queue: [
				video({ id: 1, job_id: "job-1", status: "RUNNING" }),
				video({ id: 2, job_id: "job-2", status: "PENDING", title: "Second" }),
				video({ id: 3, job_id: "job-3", status: "RUNNING", title: "Third" }),
			],
			failures: [],
		};
		state.active.data = [
			{
				video: video({ id: 1, job_id: "job-1", status: "RUNNING" }),
				part_index: 1,
				stage: "segments",
				bytes_written: 1000,
				segments_done: 37,
				segments_gaps: 0,
				segments_ad_gaps: 0,
				segments_total: 100,
				percent: 37,
				speed: "2.0 MiB/s",
				eta: "4m",
			},
		];
		render(createElement(ArchiveQueue));
		expect(screen.getByText("37%")).toBeTruthy();
		expect(screen.getByRole("progressbar").getAttribute("aria-valuenow")).toBe(
			"37",
		);
		expect(screen.getByText(/2.0 MiB\/s/)).toBeTruthy();
		expect(screen.getByText("archive.waiting")).toBeTruthy();
		// A running archive with no live sample yet shows the starting label.
		expect(screen.getByText("archive.progress_starting")).toBeTruthy();
		state.active.data = undefined;
	});

	it("hides the actions from viewers", () => {
		state.canManage = false;
		state.queue.data = { queue: [video({ status: "PENDING" })], failures: [] };
		render(createElement(ArchiveQueue));
		expect(screen.queryByRole("button", { name: "archive.remove" })).toBeNull();
		expect(screen.getByText("Streamer")).toBeTruthy();
	});

	it("lists recent failures with their error, countdown, and actions", () => {
		const inAnHour = new Date(Date.now() + 3600_000).toISOString();
		state.queue.data = {
			queue: [],
			failures: [
				video({
					id: 7,
					job_id: "job-7",
					status: "FAILED",
					title: "Flaky one",
					error: "hls fetch: transport (attempts=5)",
					next_retry_at: inAnHour,
					downloaded_at: "2026-09-08T10:00:00Z",
				}),
				video({
					id: 8,
					job_id: "job-8",
					status: "FAILED",
					title: "Gone one",
					error: "Twitch has no playable VOD with this id",
					downloaded_at: "2026-09-08T09:00:00Z",
				}),
			],
		};
		render(createElement(ArchiveQueue));
		expect(screen.getByText("archive.queue_empty")).toBeTruthy();
		expect(screen.getByText("archive.failures_title")).toBeTruthy();
		expect(screen.getByText("hls fetch: transport (attempts=5)")).toBeTruthy();
		expect(screen.getByTestId("archive-retry-in").textContent).toBe(
			"archive.retry_in",
		);
		expect(screen.getByText("archive.no_retry")).toBeTruthy();

		// Retry now is offered for every failure; cancelling only for a
		// scheduled retry.
		const retries = screen.getAllByRole("button", {
			name: "archive.retry_now",
		});
		expect(retries).toHaveLength(2);
		const cancels = screen.getAllByRole("button", {
			name: "archive.cancel_retry",
		});
		expect(cancels).toHaveLength(1);
		fireEvent.click(retries[1] as HTMLElement);
		expect(state.retry.mutate).toHaveBeenCalledWith(
			{ video_id: 8 },
			expect.anything(),
		);
		fireEvent.click(cancels[0] as HTMLElement);
		expect(state.cancelRetry.mutate).toHaveBeenCalledWith(
			{ video_id: 7 },
			expect.anything(),
		);
	});

	it("shows no failures section until the queue has loaded", () => {
		state.queue.data = undefined;
		state.queue.isLoading = true;
		render(createElement(ArchiveQueue));
		expect(screen.queryByTestId("archive-failures")).toBeNull();
		state.queue.isLoading = false;
	});

	it("surfaces a load error", () => {
		state.queue.data = undefined;
		state.queue.isError = true;
		state.queue.error = new Error("boom");
		render(createElement(ArchiveQueue));
		expect(screen.getByText(/archive.queue_failed/)).toBeTruthy();
		state.queue.isError = false;
		state.queue.error = null;
	});
});
