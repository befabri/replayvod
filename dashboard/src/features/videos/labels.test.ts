import { createInstance, type TFunction } from "i18next";
import { describe, expect, it } from "vitest";
import en from "@/i18n/locales/en.json";
import fr from "@/i18n/locales/fr.json";
import { RECORDING_QUALITIES } from "@/lib/recording-settings";
import { makeVideo, makeVideoPart } from "@/test/fixtures";
import {
	qualityLabel,
	recordingQualityLabel,
	videoQualityLabel,
} from "./labels";

async function translator(lng: string): Promise<TFunction> {
	const i18n = createInstance();
	await i18n.init({
		lng,
		resources: { en: { translation: en }, fr: { translation: fr } },
	});
	return i18n.t;
}

describe.each(["en", "fr"])("quality labels in %s", (lng) => {
	it("labels requested and selected qualities", async () => {
		const t = await translator(lng);
		for (const quality of RECORDING_QUALITIES) {
			const key = `videos.quality_${quality.toLowerCase()}`;
			expect(t(key)).not.toBe(key);
			expect(videoQualityLabel(t, { quality, is_audio_only: false })).toBe(
				t(key),
			);
		}
		for (const quality of [...RECORDING_QUALITIES, "audio_only"]) {
			expect(videoQualityLabel(t, { quality, is_audio_only: true })).toBe(
				t("videos.mode_audio"),
			);
		}
		for (const quality of ["1080p60", "720p30", "future"]) {
			expect(videoQualityLabel(t, { quality, is_audio_only: false })).toBe(
				quality,
			);
		}
	});

	it("labels the audio rendition wherever a raw quality is shown", async () => {
		const t = await translator(lng);
		expect(qualityLabel(t, "audio_only")).toBe(t("videos.mode_audio"));
		expect(qualityLabel(t, "chunked")).toBe("chunked");
	});

	it("summarizes a recording from its parts", async () => {
		const t = await translator(lng);
		const mixed = makeVideo(0, {
			quality: "HIGH",
			is_audio_only: false,
			parts: [
				makeVideoPart({ part_index: 2, quality: "720p60" }),
				makeVideoPart({ part_index: 1, quality: "1080p60" }),
				makeVideoPart({ part_index: 3, quality: "1080p60" }),
			],
		});
		expect(recordingQualityLabel(t, mixed)).toBe(
			`${t("videos.mixed_quality")} (1080p60, 720p60)`,
		);

		const single = { ...mixed, parts: [makeVideoPart({ quality: "720p60" })] };
		expect(recordingQualityLabel(t, single)).toBe("720p60");

		const queued = { ...mixed, parts: [] };
		expect(recordingQualityLabel(t, queued)).toBe(t("videos.quality_high"));

		const audio = {
			...mixed,
			is_audio_only: true,
			parts: [makeVideoPart({ quality: "audio_only" })],
		};
		expect(recordingQualityLabel(t, audio)).toBe(t("videos.mode_audio"));
		expect(recordingQualityLabel(t, { ...audio, parts: [] })).toBe(
			t("videos.mode_audio"),
		);
	});
});
