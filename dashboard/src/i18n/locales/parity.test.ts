import { describe, expect, it } from "vitest"

import en from "./en.json"
import fr from "./fr.json"

// Parity guard: every key that exists in one locale must exist in
// every other locale. react-i18next silently falls back to the default
// language on a missing key, so regressions ship as "English text
// showing up in French UI" bugs that are easy to miss in review.
//
// If this test fails:
// - new key added to en.json  → add it to fr.json
// - new key added to fr.json  → add it to en.json
// - key renamed               → rename in both
//
// Interpolation tokens ({{name}}) are also parity-checked: a
// translator who drops the placeholder silently breaks rendering.

type JSONValue = string | { [k: string]: JSONValue }

function flatten(obj: JSONValue, prefix = ""): Map<string, string> {
	const out = new Map<string, string>()
	if (typeof obj === "string") {
		out.set(prefix, obj)
		return out
	}
	for (const [k, v] of Object.entries(obj)) {
		const key = prefix ? `${prefix}.${k}` : k
		for (const [sub, val] of flatten(v, key)) {
			out.set(sub, val)
		}
	}
	return out
}

function extractTokens(s: string): string[] {
	const matches = s.match(/\{\{[^}]+\}\}/g)
	return matches ? matches.sort() : []
}

const enFlat = flatten(en)
const frFlat = flatten(fr)

describe("locale parity", () => {
	it("every en.json key exists in fr.json", () => {
		const missing = [...enFlat.keys()].filter((k) => !frFlat.has(k))
		expect(missing, `missing in fr.json:\n${missing.join("\n")}`).toEqual([])
	})

	it("every fr.json key exists in en.json", () => {
		const missing = [...frFlat.keys()].filter((k) => !enFlat.has(k))
		expect(missing, `missing in en.json:\n${missing.join("\n")}`).toEqual([])
	})

	it("interpolation tokens match between en and fr for every shared key", () => {
		const mismatches: string[] = []
		for (const [key, enVal] of enFlat) {
			const frVal = frFlat.get(key)
			if (frVal === undefined) continue
			const enTokens = extractTokens(enVal).join(",")
			const frTokens = extractTokens(frVal).join(",")
			if (enTokens !== frTokens) {
				mismatches.push(`${key}: en=[${enTokens}] fr=[${frTokens}]`)
			}
		}
		expect(mismatches, mismatches.join("\n")).toEqual([])
	})
})
