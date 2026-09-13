// @vitest-environment jsdom

import { PLAYBACK_SETTINGS } from "@/test/playback-settings";

vi.mock("@/features/settings/playback", () => ({
	usePlaybackSettings: () => PLAYBACK_SETTINGS,
}));

import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { VideoResponse } from "@/api/generated/trpc";

const useContinueWatchingMock = vi.hoisted(() =>
	vi.fn(() => ({ data: undefined as VideoResponse[] | undefined })),
);

vi.mock("react-i18next", () => ({
	useTranslation: () => ({ t: (key: string) => key }),
}));
vi.mock("@/features/videos/queries", () => ({
	useContinueWatching: useContinueWatchingMock,
}));
vi.mock("@/features/videos/permissions", () => ({
	useCanManageVideos: () => false,
}));
vi.mock("@/features/videos/components/VideoCard", async () => {
	const React = await vi.importActual<typeof import("react")>("react");
	return {
		VideoCard: ({ video }: { video: VideoResponse }) =>
			React.createElement("div", { "data-testid": "card" }, String(video.id)),
	};
});

import { ContinueWatching } from "./ContinueWatching";

afterEach(() => {
	cleanup();
	useContinueWatchingMock.mockReset();
});

function video(id: number, position: number, duration = 3600): VideoResponse {
	return {
		id,
		job_id: `job-${id}`,
		filename: `video-${id}`,
		display_name: "Channel",
		title: `Video ${id}`,
		status: "DONE",
		completion_kind: "complete",
		truncated: false,
		quality: "1080p",
		is_audio_only: false,
		broadcaster_id: "bc-1",
		viewer_count: 0,
		language: "en",
		duration_seconds: duration,
		start_download_at: "2026-01-01T00:00:00Z",
		source: "live",
		user_state: {
			watch_later: false,
			last_position_seconds: position,
			watched_at: "2026-01-01T00:00:00Z",
			updated_at: "2026-01-01T00:00:00Z",
		},
	};
}

describe("ContinueWatching", () => {
	it("renders nothing before data arrives or when nothing resumes", () => {
		useContinueWatchingMock.mockReturnValue({ data: undefined });
		const { rerender } = render(<ContinueWatching />);
		expect(screen.queryByTestId("continue-watching")).toBeNull();

		useContinueWatchingMock.mockReturnValue({ data: [] });
		rerender(<ContinueWatching />);
		expect(screen.queryByTestId("continue-watching")).toBeNull();

		// Barely started and played out both open at the start: no card.
		useContinueWatchingMock.mockReturnValue({
			data: [video(1, 3), video(2, 3590)],
		});
		rerender(<ContinueWatching />);
		expect(screen.queryByTestId("continue-watching")).toBeNull();
	});

	it("shows the resumable recordings, at most six, in the server's order", () => {
		useContinueWatchingMock.mockReturnValue({
			data: [
				video(1, 600),
				video(2, 3),
				video(3, 1200),
				video(4, 3590),
				video(5, 60),
				video(6, 70),
				video(7, 80),
				video(8, 90),
				video(9, 100),
				video(10, 110),
			],
		});
		render(<ContinueWatching />);
		expect(
			screen.getByRole("heading", { name: "dashboard.continue_watching" }),
		).toBeTruthy();
		expect(
			screen.getAllByTestId("card").map((card) => card.textContent),
		).toEqual(["1", "3", "5", "6", "7", "8"]);
	});
});
