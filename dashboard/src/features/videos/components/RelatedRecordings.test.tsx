// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { ComponentProps } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { RelatedRecordingsResponse } from "@/api/generated/trpc";

const query = vi.hoisted(() => ({
	data: undefined as RelatedRecordingsResponse | undefined,
	error: null as Error | null,
	isFetching: false,
	refetch: vi.fn(),
}));
vi.mock("../queries", () => ({ useRelatedRecordings: () => query }));
vi.mock("react-i18next", () => ({
	useTranslation: () => ({ t: (key: string) => key }),
}));
vi.mock("@tanstack/react-router", () => ({
	Link: ({
		to,
		params,
		search,
		...props
	}: ComponentProps<"a"> & {
		to: string;
		params: { videoId: string };
		search: { t?: number };
	}) => (
		<a
			href={to.replace("$videoId", params.videoId)}
			data-search={JSON.stringify(search)}
			{...props}
		/>
	),
}));

import { RelatedRecordings } from "./RelatedRecordings";

beforeEach(() => {
	query.data = {
		intent_id: "manual",
		status: "waiting",
		items: [78, 79, 80].map((id, index) => ({
			id,
			job_id: `job-${id}`,
			position: index + 1,
			title: `Recording ${id}`,
			status: "DONE",
			completion_kind: "complete",
			started_at: "2026-01-01T00:00:00Z",
		})),
	};
	query.error = null;
	query.isFetching = false;
});
afterEach(() => {
	cleanup();
	vi.clearAllMocks();
});

describe("RelatedRecordings", () => {
	it.each([
		78, 79, 80,
	])("links the correct neighbors and current row for %i", (id) => {
		render(<RelatedRecordings videoId={id} />);
		const previous = screen.queryByRole("link", {
			name: "watch.related_previous",
		});
		const next = screen.queryByRole("link", { name: "watch.related_next" });
		expect(previous?.getAttribute("href") ?? null).toBe(
			id === 78 ? null : `/dashboard/watch/${id - 1}`,
		);
		expect(next?.getAttribute("href") ?? null).toBe(
			id === 80 ? null : `/dashboard/watch/${id + 1}`,
		);
		const current = screen.getByRole("link", {
			name: new RegExp(`Recording ${id}`),
		});
		expect(current.getAttribute("aria-current")).toBe("page");
		for (const link of screen.getAllByRole("link"))
			expect(link.getAttribute("data-search")).toBe("{}");
	});

	it.each([
		"no intent",
		"empty",
		"singleton",
		"missing anchor",
	])("hides %s", (state) => {
		if (!query.data) throw new Error("Missing fixture");
		if (state === "no intent") delete query.data.intent_id;
		if (state === "empty") query.data.items = [];
		if (state === "singleton") query.data.items = query.data.items.slice(0, 1);
		render(
			<RelatedRecordings videoId={state === "missing anchor" ? 999 : 78} />,
		);
		expect(screen.queryByRole("navigation")).toBeNull();
	});

	it("offers retry when a failed refresh cannot reveal a new sibling yet", () => {
		if (!query.data) throw new Error("Missing fixture");
		query.data.items = query.data.items.slice(0, 1);
		const { rerender } = render(<RelatedRecordings videoId={78} />);
		expect(screen.queryByRole("button")).toBeNull();
		query.error = new Error("server unavailable");
		rerender(<RelatedRecordings videoId={78} />);
		expect(screen.queryByRole("navigation")).toBeNull();
		fireEvent.click(
			screen.getByRole("button", { name: "watch.related_retry" }),
		);
		expect(query.refetch).toHaveBeenCalledTimes(1);
	});

	it("retries an initial failure using the shared button", () => {
		query.data = undefined;
		query.error = new Error("offline");
		const { rerender } = render(<RelatedRecordings videoId={78} />);
		const retry = screen.getByRole("button", { name: "watch.related_retry" });
		fireEvent.click(retry);
		expect(query.refetch).toHaveBeenCalledTimes(1);
		query.isFetching = true;
		rerender(<RelatedRecordings videoId={78} />);
		expect((retry as HTMLButtonElement).disabled).toBe(true);
	});

	it("keeps the same navigation links mounted after a background failure and recovery", () => {
		const { rerender } = render(<RelatedRecordings videoId={79} />);
		const next = screen.getByRole("link", { name: "watch.related_next" });
		query.error = new Error("offline");
		rerender(<RelatedRecordings videoId={79} />);
		expect(screen.getByRole("link", { name: "watch.related_next" })).toBe(next);
		fireEvent.click(
			screen.getByRole("button", { name: "watch.related_retry" }),
		);
		expect(query.refetch).toHaveBeenCalledTimes(1);
		query.error = null;
		rerender(<RelatedRecordings videoId={79} />);
		expect(screen.getByRole("link", { name: "watch.related_next" })).toBe(next);
		expect(screen.queryByRole("button")).toBeNull();
	});

	it("uses shared cancelled labels and gives removed state precedence", () => {
		if (!query.data) throw new Error("Missing fixture");
		query.data.items[0].status = "FAILED";
		query.data.items[0].completion_kind = "cancelled";
		query.data.items[1].deleted_at = "2026-01-01T00:00:00Z";
		render(<RelatedRecordings videoId={78} />);
		expect(screen.getByText("videos.status.CANCELLED")).toBeTruthy();
		expect(screen.getByText(/watch.related_removed/)).toBeTruthy();
		expect(screen.queryByText("videos.status.FAILED")).toBeNull();
	});
});
