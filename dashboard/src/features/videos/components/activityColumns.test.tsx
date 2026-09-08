// @vitest-environment jsdom

import { cleanup, render, screen } from "@testing-library/react";
import type { TFunction } from "i18next";
import type { ComponentProps, ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { VideoResponse } from "@/api/generated/trpc";

vi.mock("@tanstack/react-router", async () => {
	const React = await vi.importActual<typeof import("react")>("react");
	type LinkProps = ComponentProps<"a"> & {
		children?: ReactNode;
		params?: unknown;
		search?: unknown;
		to?: unknown;
	};
	return {
		Link: ({
			children,
			params: _params,
			search: _search,
			to: _to,
			...props
		}: LinkProps) =>
			React.createElement("a", { href: "#", ...props }, children),
	};
});

vi.mock("@/features/videos", () => ({
	channelLabel: (video: VideoResponse) =>
		video.broadcaster_name || video.broadcaster_login || video.broadcaster_id,
}));

import {
	type HistoryView,
	historyColumns,
	MediaCell,
	RecordingCell,
} from "./activityColumns";

const t = ((key: string) => key) as unknown as TFunction;

function removed(overrides: Partial<VideoResponse> = {}): VideoResponse {
	return {
		id: 95,
		job_id: "job-95",
		filename: "20260607-145422-yuchorinchan-02625d84",
		display_name: "yuchorinchan",
		title: "Ranked climb, day 3",
		status: "DONE",
		completion_kind: "complete",
		truncated: false,
		quality: "1080p60",
		is_audio_only: false,
		broadcaster_id: "1",
		broadcaster_login: "yuchorinchan",
		broadcaster_name: "유초린",
		profile_image_url: "",
		viewer_count: 0,
		language: "ko",
		duration_seconds: 1007,
		size_bytes: 1029394911,
		start_download_at: "2026-06-07T14:54:22Z",
		source: "live",
		deleted_at: "2026-09-06T16:00:00Z",
		deletion_kind: "missing",
		...overrides,
	};
}

// live() is the same recording before anything removed it.
function live(overrides: Partial<VideoResponse> = {}): VideoResponse {
	return removed({
		deleted_at: undefined,
		deletion_kind: undefined,
		...overrides,
	});
}

afterEach(cleanup);

describe("RecordingCell", () => {
	it("shows the kept poster, the channel, and the stream title for a file-missing row", () => {
		const { container } = render(
			<RecordingCell
				row={removed({
					thumbnail:
						"thumbnails/20260607-145422-yuchorinchan-02625d84-part01.jpg",
				})}
				t={t}
			/>,
		);

		const poster = container.querySelector("img");
		expect(poster?.getAttribute("src")).toContain(
			"/api/v1/thumbnails/20260607-145422-yuchorinchan-02625d84-part01.jpg",
		);
		expect(screen.getByText("유초린").closest("a")).toBeTruthy();
		expect(screen.getByText("Ranked climb, day 3")).toBeTruthy();
	});

	it("falls back to the placeholder when the purge deleted the poster", () => {
		const { container } = render(
			<RecordingCell
				row={removed({ deletion_kind: "manual", thumbnail: undefined })}
				t={t}
			/>,
		);

		expect(container.querySelector("img")).toBeNull();
		expect(screen.getByLabelText("videos.no_thumbnail")).toBeTruthy();
	});

	it("does not repeat the channel name as a title", () => {
		render(<RecordingCell row={removed({ title: "유초린" })} t={t} />);

		expect(screen.getAllByText("유초린")).toHaveLength(1);
	});

	it("opens the player from the poster and the title while the media is there", () => {
		const { container } = render(
			<RecordingCell
				row={live({ thumbnail: "thumbnails/poster.jpg" })}
				t={t}
			/>,
		);

		expect(container.querySelector("img")?.closest("a")).toBeTruthy();
		expect(screen.getByText("Ranked climb, day 3").closest("a")).toBeTruthy();
	});

	it("leaves the poster and the title unlinked once the recording is gone", () => {
		const { container } = render(
			<RecordingCell
				row={removed({ thumbnail: "thumbnails/poster.jpg" })}
				t={t}
			/>,
		);

		expect(container.querySelector("img")?.closest("a")).toBeNull();
		expect(screen.getByText("Ranked climb, day 3").closest("a")).toBeNull();
	});
});

describe("MediaCell", () => {
	it.each([
		["missing", "history.media_missing"],
		["manual", "history.media_manual"],
		["retention", "history.media_retention"],
	])("names why a %s tombstone lost its files", (kind, key) => {
		render(<MediaCell row={removed({ deletion_kind: kind })} t={t} />);

		expect(screen.getByText(key)).toBeTruthy();
	});

	it("reports a completed recording as still on disk", () => {
		render(<MediaCell row={live()} t={t} />);

		expect(screen.getByText("history.media_present")).toBeTruthy();
	});

	it("reports no fate for a run that never produced a file", () => {
		render(<MediaCell row={live({ status: "FAILED" })} t={t} />);

		expect(screen.getByText("—")).toBeTruthy();
	});
});

describe("historyColumns", () => {
	const ids = (view: HistoryView) =>
		historyColumns(t, view, true, "en").map((col) =>
			"accessorKey" in col && col.accessorKey
				? String(col.accessorKey)
				: col.id,
		);

	it("drops the media column when the scope already answers it", () => {
		expect(ids({ outcome: "all", media: "on_disk" })).not.toContain("media");
		expect(ids({ outcome: "all", media: "any" })).toContain("media");
		expect(ids({ outcome: "all", media: "removed" })).toContain("media");
	});

	it("trades the size column for the error on failures", () => {
		const failed = ids({ outcome: "failed", media: "any" });

		expect(failed).toContain("error");
		expect(failed).not.toContain("size_bytes");
	});

	it("keeps the size column for cancellations, which can salvage parts", () => {
		const cancelled = ids({ outcome: "cancelled", media: "any" });

		expect(cancelled).toContain("size_bytes");
		expect(cancelled).not.toContain("error");
	});

	it("offers no actions once every row is a tombstone", () => {
		expect(ids({ outcome: "all", media: "removed" })).not.toContain("actions");
		expect(ids({ outcome: "all", media: "any" })).toContain("actions");
	});
});

it("reports finalized media on failed recordings", () => {
	render(
		<MediaCell
			row={removed({
				status: "FAILED",
				deleted_at: undefined,
				has_media: true,
			})}
			t={t}
		/>,
	);
	expect(screen.getByText("history.media_present")).toBeTruthy();
});
