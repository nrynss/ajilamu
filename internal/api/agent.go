package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/nrynss/ajilamu/internal/agent"
	"github.com/nrynss/ajilamu/internal/cost"
)

// EditorAgent answers questions without a ledger writer or mutation dependency.
type EditorAgent interface {
	Ask(context.Context, string, string) (agent.Reply, error)
}

// AgentHandler serves one question and preserves its measured charges.
func AgentHandler(editor EditorAgent, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			writeHistoryFailure(w, http.StatusMethodNotAllowed, "Method not allowed.")
			return
		}
		if editor == nil {
			writeHistoryFailure(w, http.StatusServiceUnavailable, "The editor agent is unavailable.")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		var request AgentRequest
		if err := decoder.Decode(&request); err != nil {
			writeHistoryFailure(w, http.StatusBadRequest, "Send one question as JSON.")
			return
		}
		if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
			writeHistoryFailure(w, http.StatusBadRequest, "Send one question as JSON.")
			return
		}
		id := strings.TrimSpace(r.PathValue("id"))
		if id == "" || strings.TrimSpace(request.Question) == "" {
			writeHistoryFailure(w, http.StatusBadRequest, "Choose a dub and enter a question.")
			return
		}
		// The dub owns its conversation. Name it in the prompt for ledger queries.
		reply, err := editor.Ask(r.Context(), id, "For dub "+id+":\n"+request.Question)
		if err != nil {
			if logger != nil {
				logger.Warn("editor agent turn failed", "error", err)
			}
			writeHistoryFailure(w, http.StatusBadGateway, "The editor agent could not answer this question.")
			return
		}
		charges := reply.Charges
		if charges == nil {
			charges = []cost.Charge{}
		}
		var total cost.Price
		for _, charge := range charges {
			total += charge.Total()
		}
		writeHistoryJSON(w, http.StatusOK, AgentResponse{Answer: reply.Text, Charges: charges, TotalNanodollars: total})
	})
}
