import fixtureJSON from "../../../testdata/wire/dub.json?raw"
import seg1 from "../../../testdata/takes/seg_1_try1.wav?url"
import seg2 from "../../../testdata/takes/seg_2_try1.wav?url"
import seg3Stretched from "../../../testdata/takes/seg_3_stretched.wav?url"
import seg3 from "../../../testdata/takes/seg_3_try1.wav?url"
import seg4Stretched from "../../../testdata/takes/seg_4_stretched.wav?url"
import seg4 from "../../../testdata/takes/seg_4_try1.wav?url"
import seg5 from "../../../testdata/takes/seg_5_try1.wav?url"
import seg6 from "../../../testdata/takes/seg_6_try1.wav?url"
import seg7 from "../../../testdata/takes/seg_7_try1.wav?url"
import seg8 from "../../../testdata/takes/seg_8_try1.wav?url"
import type { Dub, Line, Readiness, Segment, Take } from "./types"

const takeSources: Readonly<Record<string, string>> = {
  "seg_1_try1.wav": seg1,
  "seg_2_try1.wav": seg2,
  "seg_3_stretched.wav": seg3Stretched,
  "seg_3_try1.wav": seg3,
  "seg_4_stretched.wav": seg4Stretched,
  "seg_4_try1.wav": seg4,
  "seg_5_try1.wav": seg5,
  "seg_6_try1.wav": seg6,
  "seg_7_try1.wav": seg7,
  "seg_8_try1.wav": seg8
}

export interface FixtureLineRow {
  segment: Segment
  line: Line | undefined
  take: Take | undefined
}

export const FIXTURE_IDS = [
  "fixture",
  "d3bca364-9c8a-4107-8df9-c4faf909b008",
  "6d9c2f1a-3b4e-4a8d-9c1e-7f2b5a3d8c40"
] as const

export function isFixtureID(id: string | undefined): boolean {
  if (!id) return false
  return FIXTURE_IDS.includes(id as typeof FIXTURE_IDS[number])
}

/** Chrome facts come from the same fixture payload the workspace renders. */
export function fixtureChrome(projectID: string): { readiness: Readiness; total_nanodollars: number } | undefined {
  try {
    const dub = loadFixtureDub(projectID)
    if (!dub) return undefined
    return {
      readiness: dub.readiness,
      total_nanodollars: dub.total.total_nanodollars
    }
  } catch {
    return undefined
  }
}

function isFixtureDub(value: unknown): value is Dub {
  if (typeof value !== "object" || value === null) return false

  const dub = value as Partial<Dub>
  return typeof dub.id === "string"
    && typeof dub.title === "string"
    && Array.isArray(dub.segments)
    && Array.isArray(dub.languages)
    && Array.isArray(dub.charges)
    && Array.isArray(dub.commits)
    && typeof dub.total === "object"
    && dub.total !== null
}

/**
 * Loads the frozen T1.5 payload used by the offline workspace.
 * Only the fixture alias and its real id may render completed fixture work.
 */
export function loadFixtureDub(projectID: string): Dub | undefined {
  const value = JSON.parse(fixtureJSON) as unknown
  if (!isFixtureDub(value)) {
    throw new Error("The offline workspace fixture is not a complete project.")
  }

  return isFixtureID(projectID) || projectID === value.id ? value : undefined
}

/** The frozen fixture contains discrete takes rather than a pre-mixed language track. */
export function fixtureTakeSource(take: Take): string {
  return takeSources[take.file] ?? ""
}

export function fixtureLineRows(dub: Dub, language: string): FixtureLineRow[] {
  const track = dub.languages.find((candidate) => candidate.language === language)
  return dub.segments.map((segment) => {
    const line = track?.lines.find((candidate) => candidate.segment_id === segment.id)
    return { segment, line, take: line?.takes.at(-1) }
  })
}

export function pictureDurationMs(_dub: Dub): number {
  return 75_008
}

export function referenceSlotMs(dub: Dub): number {
  return Math.max(1, ...dub.segments.map((segment) => segment.duration_ms))
}
