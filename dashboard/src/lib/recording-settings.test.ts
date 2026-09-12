import { describe, expect, it } from "vitest";
import {
	ApproveRequestInputSchema,
	CreateInputSchema,
	EnqueueArchiveInputSchema,
	ScheduleUpdateInputSchema,
	TriggerDownloadInputSchema,
} from "@/api/generated/zod";
import {
	forceH264For,
	isRecordingQuality,
	qualityAboveHD,
	qualityTierForHeight,
	RECORDING_QUALITIES,
	RecordingQualitySchema,
	recordingQualityValue,
} from "./recording-settings";

// Every generated request that carries a recording quality.
// RecordingQualitySchema anchors on CreateInput; the rest have to offer the
// same values or a picker ends up offering one the endpoint behind it rejects.
const QUALITY_BEARING_SCHEMAS = {
	CreateInput: CreateInputSchema,
	ScheduleUpdateInput: ScheduleUpdateInputSchema,
	ApproveRequestInput: ApproveRequestInputSchema,
	TriggerDownloadInput: TriggerDownloadInputSchema,
	EnqueueArchiveInput: EnqueueArchiveInputSchema,
};

type ZodNode = {
	options?: unknown[];
	def?: {
		type?: string;
		innerType?: unknown;
		options?: unknown[];
		values?: unknown[];
	};
};

const strings = (values: unknown[] | undefined): string[] =>
	(values ?? []).filter((value): value is string => typeof value === "string");

// acceptedValues reads the values a generated field accepts straight off the
// schema, walking the optional/union wrappers the generator adds where older
// clients may send "". Reading them rather than probing a candidate list is
// what makes the parity check catch a value added to one endpoint only.
// An unhandled shape throws rather than reporting an empty set.
function acceptedValues(field: unknown): string[] {
	const node = field as ZodNode;
	const def = node?.def;
	switch (def?.type) {
		case "optional":
		case "nullable":
		case "default":
			return acceptedValues(def.innerType);
		case "union":
			return (def.options ?? []).flatMap(acceptedValues);
		case "enum":
			return strings(node.options);
		case "literal":
			return strings(def.values);
		default:
			throw new Error(`unhandled generated quality shape: ${def?.type}`);
	}
}

// "" is a widening for older clients, not a quality.
const acceptedQualities = (field: unknown) =>
	acceptedValues(field)
		.filter((value) => value !== "")
		.sort();

describe("recording quality", () => {
	it("offers the same values on every generated request that carries one", () => {
		const anchor = acceptedQualities(RecordingQualitySchema);
		expect(anchor.length).toBeGreaterThan(1);
		for (const [name, schema] of Object.entries(QUALITY_BEARING_SCHEMAS)) {
			expect(acceptedQualities(schema.shape.quality), name).toEqual(anchor);
		}
	});

	it("rejects a value no endpoint accepts", () => {
		expect(RecordingQualitySchema.safeParse("ULTRA").success).toBe(false);
		expect(isRecordingQuality("ULTRA")).toBe(false);
	});

	// The display list is sorted from the schema's own options, so it cannot
	// drop a quality the server added or invent one it does not accept.
	it("displays every accepted quality exactly once", () => {
		expect([...RECORDING_QUALITIES].sort()).toEqual(
			acceptedQualities(RecordingQualitySchema),
		);
		expect(new Set(RECORDING_QUALITIES).size).toBe(RECORDING_QUALITIES.length);
	});

	it("displays them best first", () => {
		expect(RECORDING_QUALITIES).toEqual([
			"BEST",
			"1440",
			"HIGH",
			"MEDIUM",
			"LOW",
		]);
	});

	it("falls back for a value written by another server version", () => {
		expect(recordingQualityValue("1440")).toBe("1440");
		expect(recordingQualityValue("ULTRA")).toBe("HIGH");
		expect(recordingQualityValue("ULTRA", "BEST")).toBe("BEST");
	});
});

describe("forceH264For", () => {
	it("clears the override for audio and keeps it for video", () => {
		expect(forceH264For("audio", true)).toBe(false);
		expect(forceH264For("video", true)).toBe(true);
		expect(forceH264For("video", false)).toBe(false);
	});
});

describe("qualityAboveHD", () => {
	it("flags only the qualities ranked above HIGH", () => {
		expect(RECORDING_QUALITIES.filter(qualityAboveHD)).toEqual([
			"BEST",
			"1440",
		]);
	});
});

describe("qualityTierForHeight", () => {
	it("files a height under the rung that contains it", () => {
		expect(
			[160, 480, 720, 936, 1080, 1440, 2160].map(qualityTierForHeight),
		).toEqual(["LOW", "LOW", "MEDIUM", "HIGH", "HIGH", "1440", "BEST"]);
	});
});
