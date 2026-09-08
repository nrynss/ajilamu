package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// eventStreamUnavailable reports a writer that cannot flush frames.
const eventStreamUnavailable = "Event streaming is unavailable."

// EventsHandler serves GET /api/dubs/{id}/events as a server-sent event stream.
// Every frame carries a per-run id, one JSON ProgressEvent, and a blank line.
func EventsHandler(runs *runRegistry) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !runs.available() {
			writeRunFailure(w, http.StatusServiceUnavailable, runUnavailable)
			return
		}
		run, ok := runs.lookup(r.PathValue("id"))
		if !ok {
			writeRunFailure(w, http.StatusNotFound, runMissing)
			return
		}
		flusher, ok := w.(http.Flusher)
		if !ok {
			writeRunFailure(w, http.StatusInternalServerError, eventStreamUnavailable)
			return
		}
		sub, terminal, finished := run.subscribe()
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK)
		flusher.Flush()

		if finished {
			if terminal != nil {
				writeEventFrame(w, *terminal)
				flusher.Flush()
			}
			return
		}
		streamSubscriber(w, r, run, sub, flusher)
	})
}

// streamSubscriber copies frames until the run closes the channel or the client
// disconnects. It writes the stored terminal frame when a drop hid it.
func streamSubscriber(w io.Writer, r *http.Request, run *dubRun, sub *runSubscriber, flusher http.Flusher) {
	var lastID uint64
	for {
		select {
		case frame, ok := <-sub.frames:
			if !ok {
				if terminal := run.terminalFrame(); terminal != nil && terminal.id > lastID {
					writeEventFrame(w, *terminal)
					flusher.Flush()
				}
				return
			}
			writeEventFrame(w, frame)
			lastID = frame.id
			flusher.Flush()
		case <-r.Context().Done():
			run.unsubscribe(sub)
			return
		}
	}
}

// writeEventFrame writes one complete frame in a single call.
// The server never writes a partial frame.
func writeEventFrame(w io.Writer, frame runFrame) {
	payload, err := json.Marshal(frame.event)
	if err != nil {
		return
	}
	_, _ = io.WriteString(w, fmt.Sprintf("id: %d\ndata: %s\n\n", frame.id, payload))
}
