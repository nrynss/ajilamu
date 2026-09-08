// Server-sent event client for the run progress stream.
// It follows /api/dubs/{projectID}/events and reports every parsed event.
//
// The browser EventSource API hides the HTTP status and owns the reconnect
// schedule, so this client streams with fetch instead. It can then tell a 503
// or a 404 from a live stream, and it can bound its own backoff. The last
// event id and the last cumulative cost survive a reconnect. The resume frame
// the server sends on reconnect refreshes both.

import type { ProgressEvent } from "./types"

// ProgressPhase names where one watch stands.
export type ProgressPhase = "connecting" | "live" | "reconnecting" | "closed" | "failed"

// ProgressUpdate reports a phase change with a complete sentence when the
// change needs explaining. Connecting, live, and closed updates carry an
// empty sentence.
export interface ProgressUpdate {
  /** Phase names the current state of the watch. */
  phase: ProgressPhase
  /** Sentence explains a reconnect or a failure in plain words. */
  sentence: string
}

// ProgressHandlers receives every parsed event and every phase change.
export interface ProgressHandlers {
  /** OnEvent runs once per parsed ProgressEvent, terminal events included. */
  onEvent?: (event: ProgressEvent) => void
  /** OnPhase runs on every phase change. */
  onPhase?: (update: ProgressUpdate) => void
}

// ProgressSubscription controls one watch. Closing is safe at any time.
export interface ProgressSubscription {
  /** Close stops the watch and any pending reconnect. */
  close(): void
  /** LastEventID returns the id of the last accepted frame. */
  lastEventID(): number
  /** LastCost returns the last cumulative cost the stream carried. */
  lastCost(): number | undefined
}

// Bounded backoff for a dropped stream. The delay starts at half a second and
// doubles up to an eight-second ceiling. Six failed attempts end the watch.
const baseReconnectDelayMs = 500
const maxReconnectDelayMs = 8000
const maxReconnectAttempts = 6

// The statuses that end a watch before any stream opens.
const unavailableStatus = 503
const missingRunStatus = 404

// SentenceForStatus explains why the events route refused the stream.
function sentenceForStatus(status: number): string {
  if (status === unavailableStatus) {
    return "Runs are unavailable, so this project cannot show progress."
  }
  if (status === missingRunStatus) {
    return "No run exists for this project yet, so there is no progress to show."
  }
  return "We could not read this run's progress, so this page will not update."
}

// watchRunProgress follows one run's event stream.
// A first connection that fails ends the watch with a sentence and no retry.
// Only a stream that opened and then dropped reconnects, with bounded backoff.
export function watchRunProgress(projectID: string, handlers: ProgressHandlers = {}): ProgressSubscription {
  const path = `/api/dubs/${encodeURIComponent(projectID)}/events`
  let lastEventID = 0
  let lastCost: number | undefined
  let attempts = 0
  let streamed = false
  let closed = false
  let terminal = false
  let controller: AbortController | undefined
  let timer: number | undefined

  function report(phase: ProgressPhase, sentence: string): void {
    handlers.onPhase?.({ phase, sentence })
  }

  function stop(): void {
    closed = true
    if (timer !== undefined) {
      clearTimeout(timer)
      timer = undefined
    }
    controller?.abort()
    controller = undefined
  }

  function fail(sentence: string): void {
    stop()
    report("failed", sentence)
  }

  function scheduleReconnect(): void {
    if (closed) return
    if (attempts >= maxReconnectAttempts) {
      fail("The progress stream kept dropping, so this page stopped updating the run.")
      return
    }
    const delay = Math.min(baseReconnectDelayMs * 2 ** attempts, maxReconnectDelayMs)
    attempts += 1
    report("reconnecting", "The progress stream dropped, so we are reconnecting to the run.")
    timer = setTimeout(() => {
      timer = undefined
      void connect()
    }, delay)
  }

  // handleBlock parses one frame. A repeated or older id is ignored, so a
  // reconnect never replays a frame this watch already saw.
  function handleBlock(block: string): void {
    let id: number | undefined
    const data: string[] = []
    for (const raw of block.split("\n")) {
      const line = raw.endsWith("\r") ? raw.slice(0, -1) : raw
      if (line.startsWith(":")) continue
      if (line.startsWith("id:")) {
        const value = Number(line.slice(3).trim())
        if (Number.isInteger(value)) id = value
      } else if (line.startsWith("data:")) {
        data.push(line.slice(5).replace(/^ /, ""))
      }
    }
    if (data.length === 0) return
    if (id !== undefined) {
      if (id <= lastEventID) return
      lastEventID = id
    }

    let event: ProgressEvent
    try {
      event = JSON.parse(data.join("\n")) as ProgressEvent
    } catch {
      return
    }

    if (typeof event.total_nanodollars === "number" && Number.isFinite(event.total_nanodollars)) {
      lastCost = event.total_nanodollars
    } else if (lastCost !== undefined) {
      event.total_nanodollars = lastCost
    }

    handlers.onEvent?.(event)
    if (event.type === "done" || event.type === "error") {
      terminal = true
      report("closed", "")
      stop()
    }
  }

  async function connect(): Promise<void> {
    if (closed) return
    report("connecting", "")
    const active = new AbortController()
    controller = active

    let response: Response
    try {
      response = await fetch(path, {
        headers: { Accept: "text/event-stream" },
        signal: active.signal
      })
    } catch {
      if (closed) return
      if (streamed) scheduleReconnect()
      else fail("We could not reach the run progress stream, so this page will not update.")
      return
    }
    if (closed) return

    if (!response.ok) {
      fail(sentenceForStatus(response.status))
      return
    }

    const body = response.body
    if (!body) {
      fail("The run progress stream had no body, so this page will not update.")
      return
    }

    streamed = true
    attempts = 0
    report("live", "")

    const reader = body.getReader()
    const decoder = new TextDecoder()
    let buffer = ""
    try {
      for (;;) {
        const { done, value } = await reader.read()
        if (done) break
        buffer += decoder.decode(value, { stream: true })
        let boundary = buffer.indexOf("\n\n")
        while (boundary >= 0) {
          const block = buffer.slice(0, boundary)
          buffer = buffer.slice(boundary + 2)
          handleBlock(block)
          if (terminal || closed) return
          boundary = buffer.indexOf("\n\n")
        }
      }
    } catch {
      // A read error is a dropped stream, so the reconnect rule below decides.
    }

    if (closed || terminal) return
    scheduleReconnect()
  }

  void connect()

  return {
    close(): void {
      if (closed) return
      stop()
      report("closed", "")
    },
    lastEventID(): number {
      return lastEventID
    },
    lastCost(): number | undefined {
      return lastCost
    }
  }
}
