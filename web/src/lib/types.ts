// Wire contract types for the Ajilamu web client.
// They mirror internal/api/wire.go one to one, with the same names,
// the same snake_case JSON keys, and the same value semantics.
// The parity test in internal/api/wire_test.go fails the build when
// the Go JSON tags and these property names drift apart.
//
// Durations are integer milliseconds. Timestamps are RFC 3339 strings.
// Money is always an exact integer in nanodollars, never a float.

// Nanodollars is an exact money amount in billionths of a dollar.
// The backend sends these values as JSON integer numbers.
// Values stay below 2^53, so plain numbers stay exact.
export type Nanodollars = number

// Readiness reports the processing state of a dub.
export type Readiness = "pending" | "running" | "review" | "ready"

// FitState names how a take relates to its slot.
export type FitState = "fits" | "too_long" | "too_short"

// RepairKind names the strategy that produced a take file.
export type RepairKind = "none" | "atempo" | "rewrite" | "manual"

// ChargeKind names the billed API operation.
export type ChargeKind = "segment" | "translate" | "synthesize" | "agent"

// CommitAuthor names who caused a commit.
export type CommitAuthor = "agent" | "command_bar" | "manual_ui"

// CommitAction names what a commit changed.
// The vocabulary follows observations.md section 5, with text_corrected
// added for transcript edits on the timeline.
export type CommitAction =
  | "segment_created"
  | "boundary_nudged"
  | "speaker_reassigned"
  | "text_corrected"
  | "take_rendered"
  | "atempo_stretched"
  | "line_rewritten"
  | "user_command"

// EventType discriminates progress notifications.
export type EventType = "progress" | "done" | "error"

// EventStage names the pipeline step behind an event.
export type EventStage =
  | "segmenting"
  | "translating"
  | "synthesizing"
  | "measuring"
  | "repairing"
  | "assembling"
  | "exporting"

// DubIndex lists dubs for the home route, newest first.
export interface DubIndex {
  /** Dubs holds one summary per dub. */
  dubs: DubSummary[]
}

// DubSummary is one row in the dub index.
export interface DubSummary {
  /** ID identifies the dub. */
  id: string
  /** Title names the project for the creator. */
  title: string
  /** Languages lists target language codes. */
  languages: string[]
  /** Readiness reports the processing state. */
  readiness: Readiness
  /** TotalNanodollars is the running cost so far. */
  total_nanodollars: Nanodollars
  /** CreatedAt is the creation time as RFC 3339. */
  created_at: string
  /** UpdatedAt is the last change time as RFC 3339. */
  updated_at: string
}

// Dub is the full workspace payload for one project.
// One Dub payload drives the timeline and all three rail tabs.
export interface Dub {
  /** ID identifies the dub. */
  id: string
  /** Title names the project for the creator. */
  title: string
  /** SourceLanguage is the film language code. */
  source_language: string
  /** Readiness reports the processing state. */
  readiness: Readiness
  /** Segments lists the source track lines. */
  segments: Segment[]
  /** Languages holds one track per target language. */
  languages: LanguageTrack[]
  /** Charges lists whole-pass charges that no single take owns. */
  charges: Charge[]
  /** Total states the running cost and what it covers. */
  total: Total
  /** Commits forms the history DAG, oldest first. */
  commits: Commit[]
  /** CreatedAt is the creation time as RFC 3339. */
  created_at: string
  /** UpdatedAt is the last change time as RFC 3339. */
  updated_at: string
}

// Segment is one source-language dialogue block.
export interface Segment {
  /** ID numbers the line inside the dub. */
  id: number
  /** StartMs locates the slot start in the film. */
  start_ms: number
  /** EndMs locates the slot end in the film. */
  end_ms: number
  /** DurationMs is the slot length, end minus start. */
  duration_ms: number
  /** Text is the transcribed source line. */
  text: string
  /** Speaker names the person talking. */
  speaker: string
  /** Emotion describes how the line is spoken. */
  emotion: string
}

// LanguageTrack carries one target language.
export interface LanguageTrack {
  /** Language is the target language code. */
  language: string
  /** Lines holds one entry per source segment. */
  lines: Line[]
}

// Line is one translated line bound to a segment.
export interface Line {
  /** SegmentID binds the line to a source segment. */
  segment_id: number
  /** Text is the current target-language text. */
  text: string
  /** Flagged marks a line that no attempt could fit. */
  flagged: boolean
  /** Takes lists attempts oldest first. The last take is active. */
  takes: Take[]
}

// Take is one recorded attempt for a line.
export interface Take {
  /** File is the asset path of the take audio. */
  file: string
  /** Voice names the voice used for this take. */
  voice: string
  /** Attempt orders takes within the line, starting at one. */
  attempt: number
  /** Repair names the strategy that produced this file. */
  repair: RepairKind
  /** StretchFactorMilli is the atempo ratio in thousandths.
   * A value of 1075 means playback at 107.5 percent speed. */
  stretch_factor_milli?: number
  /** Fit measures this take against its slot. */
  fit: Fit
  /** Charges itemizes what this attempt cost. */
  charges: Charge[]
  /** Peaks holds 64 to 128 waveform values when present.
   * Each value is an unsigned byte normalized to 0 through 255. */
  peaks?: number[]
}

// Fit compares one take against its dialogue slot.
// It mirrors internal/types.Fit with integer milliseconds.
export interface Fit {
  /** SlotMs is the allowed slot length. */
  slot_ms: number
  /** MeasuredMs is the take length as measured. */
  measured_ms: number
  /** DeltaMs is measured minus slot, signed.
   * Positive delta means long. Negative delta means short. */
  delta_ms: number
  /** State reports fits, too_long, or too_short. */
  state: FitState
}

// Charge is one billed API call.
// Money values are exact nanodollar integers, never floats.
export interface Charge {
  /** Kind names the billed operation. */
  kind: ChargeKind
  /** SegmentID names the line a charge serves. Null marks whole-pass work. */
  segment_id: number | null
  /** TakeFile names the attempt a charge serves. Empty marks whole-pass work. */
  take_file: string
  /** Units counts billed characters or tokens. */
  units: number
  /** UnitPriceNanodollars is the price of one unit. */
  unit_price_nanodollars: Nanodollars
  /** TotalNanodollars is units times unit price, exact. */
  total_nanodollars: Nanodollars
}

// Total states the running project cost.
export interface Total {
  /** TotalNanodollars is the exact running sum. */
  total_nanodollars: Nanodollars
  /** Covers explains in words what the total includes. */
  covers: string
}

// Commit is one node in the history DAG.
export interface Commit {
  /** CommitID identifies the node. */
  commit_id: string
  /** ParentCommitID links the previous node. Empty marks the root. */
  parent_commit_id: string
  /** VersionNumber counts commits along the chain. */
  version_number: number
  /** CreatedAt is the commit time as RFC 3339. */
  created_at: string
  /** Action names what the commit changed. */
  action: CommitAction
  /** Author names who caused the change. */
  author: CommitAuthor
  /** Instruction keeps the original command verbatim when one exists. */
  instruction: string
}

// DubHistory is the commit DAG payload for one dub.
export interface DubHistory {
  /** Commits lists every commit, oldest first. */
  commits: Commit[]
}

// TimelineView is the timeline snapshot at one commit.
export interface TimelineView {
  /** CommitID names the commit the state comes from. */
  commit_id: string
  /** Language names the target language track. */
  language: string
  /** Segments lists the line state at that commit. */
  segments: TimelineEntry[]
}

// TimelineEntry is one line snapshot at one commit.
export interface TimelineEntry {
  /** SegmentIndex numbers the line inside the dub. */
  segment_index: number
  /** StartMs locates the slot start in the film. */
  start_ms: number
  /** EndMs locates the slot end in the film. */
  end_ms: number
  /** Speaker names the person talking. */
  speaker: string
  /** Emotion describes how the line is spoken. */
  emotion: string
  /** SourceText is the transcribed source line. */
  source_text: string
  /** Text is the target-language line. */
  text: string
  /** TakeID names the active take. */
  take_id: string
  /** VersionSeq orders the snapshot state. */
  version_seq: number
}

// BranchComparison holds metrics for two heads of one language track.
export interface BranchComparison {
  /** A reports the first head. */
  a: BranchSummary
  /** B reports the second head. */
  b: BranchSummary
}

// BranchSummary reports one head of a language track.
export interface BranchSummary {
  /** CommitID identifies the head. */
  commit_id: string
  /** Branch names the branch label. */
  branch: string
  /** SlotMs sums the reconstructed slot lengths. */
  slot_ms: number
  /** TakeCount counts the takes at the head. */
  take_count: number
  /** AttributedCostUSD sums attributed charges as a decimal string. */
  attributed_cost_usd: string
}

// ProgressEvent is one server-sent notification body.
// Every event carries a complete sentence and the running cost.
export interface ProgressEvent {
  /** Type discriminates progress, done, and error events. */
  type: EventType
  /** Stage names the pipeline step behind the event. */
  stage: EventStage
  /** Sentence is a complete natural-language update. */
  sentence: string
  /** TotalNanodollars is the running project cost so far. */
  total_nanodollars: Nanodollars
  /** SegmentID names the line in progress. Zero marks none. */
  segment_id: number
  /** Language names the target language involved. */
  language: string
  /** TakeFile names the attempt involved when one exists. */
  take_file: string
}

// CatalogSource names where the supported-language list came from.
export type CatalogSource = "committed" | "provider"

// LanguageCatalog is the supported-language list the create screen offers.
// GET /api/languages serves it, and POST /api/languages/refresh replaces it.
export interface LanguageCatalog {
  /** Languages lists supported BCP-47 codes, sorted. */
  languages: string[]
  /** Source names where the list came from. */
  source: CatalogSource
  /** FetchedAt is the RFC 3339 time of the last provider fetch.
   * It is absent for the committed list. */
  fetched_at?: string
}

// AgentCostCharge mirrors internal/cost.Charge without changing its field names.
// Kind uses the Go enum: segment 0, translate 1, synthesize 2, agent 3.
export type AgentCostCharge = {
  Kind: 0 | 1 | 2 | 3
  TakeID: number
  Units: number
  UnitPrice: Nanodollars
  PromptTokens: number
  CandidateTokens: number
  PromptUnitPrice: Nanodollars
  CandidateUnitPrice: Nanodollars
}

// AgentRequest carries the creator's question.
export interface AgentRequest {
  question: string
}

// AgentResponse reports this turn's answer and measured cost.
export interface AgentResponse {
  answer: string
  charges: AgentCostCharge[]
  total_nanodollars: Nanodollars
}
