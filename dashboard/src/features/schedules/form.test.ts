import { describe, expect, it } from "vitest";
import { buildSchedulePayload } from "./form";
import type { ScheduleFormValues } from "./schema";

const base: ScheduleFormValues = {
	broadcaster_id: "123456",
	recording_type: "video",
	quality: "HIGH",
	force_h264: false,
	has_min_viewers: false,
	min_viewers: undefined,
	has_categories: false,
	category_ids: [],
	has_tags: false,
	tag_ids: [],
	is_delete_rediff: false,
	time_before_delete: undefined,
};

describe("buildSchedulePayload", () => {
	it("clears stale force_h264 for audio payloads", () => {
		const payload = buildSchedulePayload({
			...base,
			recording_type: "audio",
			quality: "MEDIUM",
			force_h264: true,
		});
		expect(payload.recording_type).toBe("audio");
		expect(payload.quality).toBe("MEDIUM");
		expect(payload.force_h264).toBe(false);
	});

	it("drops min_viewers when the toggle is off", () => {
		expect(
			buildSchedulePayload({ ...base, has_min_viewers: false, min_viewers: 5 })
				.min_viewers,
		).toBeUndefined();
		expect(
			buildSchedulePayload({ ...base, has_min_viewers: true, min_viewers: 5 })
				.min_viewers,
		).toBe(5);
	});

	it("drops time_before_delete when retention is off", () => {
		expect(
			buildSchedulePayload({
				...base,
				is_delete_rediff: false,
				time_before_delete: 24,
			}).time_before_delete,
		).toBeUndefined();
		expect(
			buildSchedulePayload({
				...base,
				is_delete_rediff: true,
				time_before_delete: 24,
			}).time_before_delete,
		).toBe(24);
	});

	it("empties the category and tag allowlists when their toggles are off", () => {
		const payload = buildSchedulePayload({
			...base,
			has_categories: false,
			category_ids: ["a"],
			has_tags: false,
			tag_ids: [1],
		});
		expect(payload.category_ids).toEqual([]);
		expect(payload.tag_ids).toEqual([]);
	});
});
