import { describe, expect, it } from "vitest";
import {
	MAX_RETENTION_WINDOW_HOURS,
	ScheduleFormSchema,
	type ScheduleFormValues,
} from "./schema";

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

describe("ScheduleFormSchema retention validation", () => {
	it("accepts disabled retention without a window", () => {
		expect(ScheduleFormSchema.safeParse(base).success).toBe(true);
	});

	it("accepts stale disabled retention window values", () => {
		expect(
			ScheduleFormSchema.safeParse({
				...base,
				time_before_delete: 0,
			}).success,
		).toBe(true);
		expect(
			ScheduleFormSchema.safeParse({
				...base,
				time_before_delete: MAX_RETENTION_WINDOW_HOURS + 1,
			}).success,
		).toBe(true);
	});

	it("requires a window when retention is enabled", () => {
		expect(
			ScheduleFormSchema.safeParse({
				...base,
				is_delete_rediff: true,
				time_before_delete: undefined,
			}).success,
		).toBe(false);
	});

	it("rejects zero when retention is enabled", () => {
		expect(
			ScheduleFormSchema.safeParse({
				...base,
				is_delete_rediff: true,
				time_before_delete: 0,
			}).success,
		).toBe(false);
	});

	it("accepts the backend maximum retention window", () => {
		expect(
			ScheduleFormSchema.safeParse({
				...base,
				is_delete_rediff: true,
				time_before_delete: MAX_RETENTION_WINDOW_HOURS,
			}).success,
		).toBe(true);
	});

	it("rejects above the backend maximum retention window", () => {
		expect(
			ScheduleFormSchema.safeParse({
				...base,
				is_delete_rediff: true,
				time_before_delete: MAX_RETENTION_WINDOW_HOURS + 1,
			}).success,
		).toBe(false);
	});

	it("accepts the minimum window of 1 hour", () => {
		expect(
			ScheduleFormSchema.safeParse({
				...base,
				is_delete_rediff: true,
				time_before_delete: 1,
			}).success,
		).toBe(true);
	});

	it("rejects a fractional window (backend stores int64 hours)", () => {
		const result = ScheduleFormSchema.safeParse({
			...base,
			is_delete_rediff: true,
			time_before_delete: 1.5,
		});
		expect(result.success).toBe(false);
		if (!result.success) {
			expect(
				result.error.issues.some((i) => i.path.includes("time_before_delete")),
			).toBe(true);
		}
	});
});

describe("ScheduleFormSchema category and tag filter validation", () => {
	it("accepts empty category and tag arrays when their filters are disabled", () => {
		expect(
			ScheduleFormSchema.safeParse({
				...base,
				has_categories: false,
				category_ids: [],
				has_tags: false,
				tag_ids: [],
			}).success,
		).toBe(true);
	});

	it("requires at least one category when the category filter is enabled", () => {
		const result = ScheduleFormSchema.safeParse({
			...base,
			has_categories: true,
			category_ids: [],
		});
		expect(result.success).toBe(false);
		if (!result.success) {
			expect(
				result.error.issues.some((i) => i.path.includes("category_ids")),
			).toBe(true);
		}
	});

	it("rejects blank category IDs", () => {
		const result = ScheduleFormSchema.safeParse({
			...base,
			has_categories: true,
			category_ids: ["game-1", " "],
		});
		expect(result.success).toBe(false);
		if (!result.success) {
			expect(
				result.error.issues.some((i) => i.path.includes("category_ids")),
			).toBe(true);
		}
	});

	it("requires at least one tag when the tag filter is enabled", () => {
		const result = ScheduleFormSchema.safeParse({
			...base,
			has_tags: true,
			tag_ids: [],
		});
		expect(result.success).toBe(false);
		if (!result.success) {
			expect(result.error.issues.some((i) => i.path.includes("tag_ids"))).toBe(
				true,
			);
		}
	});

	it("rejects non-positive tag IDs", () => {
		const result = ScheduleFormSchema.safeParse({
			...base,
			has_tags: true,
			tag_ids: [0],
		});
		expect(result.success).toBe(false);
		if (!result.success) {
			expect(result.error.issues.some((i) => i.path.includes("tag_ids"))).toBe(
				true,
			);
		}
	});

	it("accepts selected category and tag filters", () => {
		expect(
			ScheduleFormSchema.safeParse({
				...base,
				has_categories: true,
				category_ids: ["game-1"],
				has_tags: true,
				tag_ids: [1],
			}).success,
		).toBe(true);
	});
});
