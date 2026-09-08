import { Buffer } from "node:buffer";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import type { Route } from "@playwright/test";

// A four second AAC clip and a byte-range responder for it, shared by the
// watch page specs so a real <audio> element can load, play, and fail.
export const audioDurationSeconds = 4;
export const audioFixture = Buffer.from(
	[
		"AAAAHGZ0eXBNNEEgAAACAE00QSBpc29taXNvMgAABbNtb292AAAAbG12aGQAAAAAAAAAAAAAAAAAAAPoAAAPoAABAAABAAAA",
		"AAAAAAAAAAAAAQAAAAAAAAAAAAAAAAAAAAEAAAAAAAAAAAAAAAAAAEAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAC",
		"AAAE3XRyYWsAAABcdGtoZAAAAAMAAAAAAAAAAAAAAAEAAAAAAAAPoAAAAAAAAAAAAAAAAQEAAAAAAQAAAAAAAAAAAAAAAAAA",
		"AAEAAAAAAAAAAAAAAAAAAEAAAAAAAAAAAAAAAAAAACRlZHRzAAAAHGVsc3QAAAAAAAAAAQAAD6AAAAQAAAEAAAAABFVtZGlh",
		"AAAAIG1kaGQAAAAAAAAAAAAAAAAAAKxEAAK1EFXEAAAAAAAtaGRscgAAAAAAAAAAc291bgAAAAAAAAAAAAAAAFNvdW5kSGFu",
		"ZGxlcgAAAAQAbWluZgAAABBzbWhkAAAAAAAAAAAAAAAkZGluZgAAABxkcmVmAAAAAAAAAAEAAAAMdXJsIAAAAAEAAAPEc3Ri",
		"bAAAAGpzdHNkAAAAAAAAAAEAAABabXA0YQAAAAAAAAABAAAAAAAAAAAAAQAQAAAAAKxEAAAAAAA2ZXNkcwAAAAADgICAJQAB",
		"AASAgIAXQBUAAAAAAB9AAAAFiQWAgIAFEghW5QAGgICAAQIAAAAgc3R0cwAAAAAAAAACAAAArQAABAAAAAABAAABEAAAABxz",
		"dHNjAAAAAAAAAAEAAAABAAAArgAAAAEAAALMc3RzegAAAAAAAAAAAAAArgAAABUAAAAEAAAABAAAAAQAAAAEAAAABAAAAAQA",
		"AAAEAAAABAAAAAQAAAAEAAAABAAAAAQAAAAEAAAABAAAAAQAAAAEAAAABAAAAAQAAAAEAAAABAAAAAQAAAAEAAAABAAAAAQA",
		"AAAEAAAABAAAAAQAAAAEAAAABAAAAAQAAAAEAAAABAAAAAQAAAAEAAAABAAAAAQAAAAEAAAABAAAAAQAAAAEAAAABAAAAAQA",
		"AAAEAAAABAAAAAQAAAAEAAAABAAAAAQAAAAEAAAABAAAAAQAAAAEAAAABAAAAAQAAAAEAAAABAAAAAQAAAAEAAAABAAAAAQA",
		"AAAEAAAABAAAAAQAAAAEAAAABAAAAAQAAAAEAAAABAAAAAQAAAAEAAAABAAAAAQAAAAEAAAABAAAAAQAAAAEAAAABAAAAAQA",
		"AAAEAAAABAAAAAQAAAAEAAAABAAAAAQAAAAEAAAABAAAAAQAAAAEAAAABAAAAAQAAAAEAAAABAAAAAQAAAAEAAAABAAAAAQA",
		"AAAEAAAABAAAAAQAAAAEAAAABAAAAAQAAAAEAAAABAAAAAQAAAAEAAAABAAAAAQAAAAEAAAABAAAAAQAAAAEAAAABAAAAAQA",
		"AAAEAAAABAAAAAQAAAAEAAAABAAAAAQAAAAEAAAABAAAAAQAAAAEAAAABAAAAAQAAAAEAAAABAAAAAQAAAAEAAAABAAAAAQA",
		"AAAEAAAABAAAAAQAAAAEAAAABAAAAAQAAAAEAAAABAAAAAQAAAAEAAAABAAAAAQAAAAEAAAABAAAAAQAAAAEAAAABAAAAAQA",
		"AAAEAAAABAAAAAQAAAAEAAAABAAAAAQAAAAEAAAABAAAAAQAAAAEAAAABAAAAAQAAAAEAAAABAAAAAQAAAAEAAAABAAAAAQA",
		"AAAEAAAABAAAAAQAAAAEAAAABAAAABRzdGNvAAAAAAAAAAEAAAXfAAAAGnNncGQBAAAAcm9sbAAAAAIAAAAB//8AAAAcc2Jn",
		"cAAAAAByb2xsAAAAAQAAAK4AAAABAAAAYnVkdGEAAABabWV0YQAAAAAAAAAhaGRscgAAAAAAAAAAbWRpcmFwcGwAAAAAAAAA",
		"AAAAAAAtaWxzdAAAACWpdG9vAAAAHWRhdGEAAAABAAAAAExhdmY2Mi4xMi4xMDEAAAAIZnJlZQAAAtFtZGF03gIATGF2YzYy",
		"LjI4LjEwMQACMEAOARggBwEYIAcBGCAHARggBwEYIAcBGCAHARggBwEYIAcBGCAHARggBwEYIAcBGCAHARggBwEYIAcBGCAH",
		"ARggBwEYIAcBGCAHARggBwEYIAcBGCAHARggBwEYIAcBGCAHARggBwEYIAcBGCAHARggBwEYIAcBGCAHARggBwEYIAcBGCAH",
		"ARggBwEYIAcBGCAHARggBwEYIAcBGCAHARggBwEYIAcBGCAHARggBwEYIAcBGCAHARggBwEYIAcBGCAHARggBwEYIAcBGCAH",
		"ARggBwEYIAcBGCAHARggBwEYIAcBGCAHARggBwEYIAcBGCAHARggBwEYIAcBGCAHARggBwEYIAcBGCAHARggBwEYIAcBGCAH",
		"ARggBwEYIAcBGCAHARggBwEYIAcBGCAHARggBwEYIAcBGCAHARggBwEYIAcBGCAHARggBwEYIAcBGCAHARggBwEYIAcBGCAH",
		"ARggBwEYIAcBGCAHARggBwEYIAcBGCAHARggBwEYIAcBGCAHARggBwEYIAcBGCAHARggBwEYIAcBGCAHARggBwEYIAcBGCAH",
		"ARggBwEYIAcBGCAHARggBwEYIAcBGCAHARggBwEYIAcBGCAHARggBwEYIAcBGCAHARggBwEYIAcBGCAHARggBwEYIAcBGCAH",
		"ARggBwEYIAcBGCAHARggBwEYIAcBGCAHARggBwEYIAcBGCAHARggBwEYIAcBGCAHARggBwEYIAcBGCAHARggBwEYIAcBGCAH",
		"ARggBwEYIAcBGCAHARggBwEYIAcBGCAHARggBwEYIAcBGCAHARggBwEYIAcBGCAHARggBwEYIAcBGCAHARggBwEYIAcBGCAH",
		"ARggBwEYIAcBGCAHARggBwEYIAcBGCAHARggBwEYIAcBGCAHARggBwEYIAcBGCAHARggBwEYIAc=",
	].join(""),
	"base64",
);

export function fulfillAudioFixture(route: Route) {
	return fulfillRangeFixture(route, audioFixture, "audio/mp4");
}

// silentWavFixture builds a PCM WAV of silence (8 kHz, 8-bit, mono) for specs
// that need more playback than the four second clip above; resume only kicks
// in five seconds into a recording. Chromium decodes it natively.
export function silentWavFixture(seconds: number) {
	const sampleRate = 8000;
	const dataLength = seconds * sampleRate;
	const header = Buffer.alloc(44);
	header.write("RIFF", 0);
	header.writeUInt32LE(36 + dataLength, 4);
	header.write("WAVE", 8);
	header.write("fmt ", 12);
	header.writeUInt32LE(16, 16);
	header.writeUInt16LE(1, 20);
	header.writeUInt16LE(1, 22);
	header.writeUInt32LE(sampleRate, 24);
	header.writeUInt32LE(sampleRate, 28);
	header.writeUInt16LE(1, 32);
	header.writeUInt16LE(8, 34);
	header.write("data", 36);
	header.writeUInt32LE(dataLength, 40);
	return Buffer.concat([header, Buffer.alloc(dataLength, 0x80)]);
}

// fulfillRangeFixture answers a media request the way the stream route does:
// HEAD with the length, byte ranges as 206, anything else as the whole body.
export async function fulfillRangeFixture(
	route: Route,
	fixture: Buffer,
	contentType: string,
) {
	const range = route.request().headers().range;
	const baseHeaders = {
		"Accept-Ranges": "bytes",
		"Content-Type": contentType,
	};
	if (route.request().method() === "HEAD") {
		await route.fulfill({
			status: 200,
			headers: {
				...baseHeaders,
				"Content-Length": String(fixture.length),
			},
		});
		return;
	}
	if (range) {
		const match = /^bytes=(\d*)-(\d*)$/.exec(range);
		const start = match?.[1] ? Number(match[1]) : 0;
		const requestedEnd = match?.[2] ? Number(match[2]) : fixture.length - 1;
		const end = Math.min(requestedEnd, fixture.length - 1);
		if (!match || start < 0 || start > end || start >= fixture.length) {
			await route.fulfill({
				status: 416,
				headers: {
					...baseHeaders,
					"Content-Range": `bytes */${fixture.length}`,
				},
			});
			return;
		}
		const body = fixture.subarray(start, end + 1);
		await route.fulfill({
			status: 206,
			headers: {
				...baseHeaders,
				"Content-Length": String(body.length),
				"Content-Range": `bytes ${start}-${end}/${fixture.length}`,
			},
			body,
		});
		return;
	}
	await route.fulfill({
		status: 200,
		headers: {
			...baseHeaders,
			"Content-Length": String(fixture.length),
		},
		body: fixture,
	});
}

// videoFixture is a thirty second H.264/AAC clip generated with ffmpeg
// (tests/fixtures/resume-30s.mp4), the container the server records.
export const videoFixture = readFileSync(
	resolve(process.cwd(), "tests/fixtures/resume-30s.mp4"),
);
