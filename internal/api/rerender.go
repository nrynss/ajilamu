package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/nrynss/ajilamu/internal/cost"
	"github.com/nrynss/ajilamu/internal/tts"
	"github.com/nrynss/ajilamu/internal/types"
)

// LineRenderer re-runs one dialogue line through the fit loop.
//
// internal/api cannot import internal/fit, which imports this package for the
// wire types. cmd/ajilamu/main.go adapts the fit loop onto this seam instead.
type LineRenderer interface {
	// RenderLine fits one line and writes its new take to TakeFile.
	RenderLine(ctx context.Context, req LineRenderRequest) (LineRenderResult, error)
}

// LineRenderRequest is one dialogue line to re-run through the fit loop.
type LineRenderRequest struct {
	// DubID identifies the project the line belongs to.
	DubID string
	// Language is the resolved BCP-47 target tag.
	Language string
	// SourceLanguage is the film's source language code. Empty means unknown,
	// so the translator prompt names no source language.
	SourceLanguage string
	// Segment is the line to re-run. Its speaker is already the requested one.
	Segment types.Segment
	// Text is the authoritative target text. The loop speaks it on every
	// attempt and never translates. An empty value asks the loop to
	// translate the source line instead.
	Text string
	// WorkDir holds the project's take files.
	WorkDir string
	// TakeFile is the absolute path of the new first take. Later attempts
	// extend its try number, so no attempt ever names an existing take.
	TakeFile string
}

// LineRenderResult is what the fit loop produced for one re-rendered line.
type LineRenderResult struct {
	// Take is the attempt the loop chose.
	Take types.Take
	// Text is the spoken target text of the chosen attempt.
	Text string
	// Voice names the voice profile that rendered the take.
	Voice string
	// Repair names the strategy that produced the take file.
	Repair types.Repair
	// RepairDetail explains an atempo ratio or a rewrite.
	RepairDetail string
	// Flagged reports that no attempt fit the slot.
	Flagged bool
	// Charges itemizes the API calls this line paid for.
	Charges []cost.Charge
	// Total is the exact price of those charges.
	Total cost.Price
	// Peaks sketches the take waveform for the timeline.
	Peaks []uint8
}

// CorrectedRecorder records a take produced from a corrected source line.
//
// The route records that take under the text_corrected action with the
// manual_ui author, so a plain RunRecorder cannot serve the source path.
// The production adapter in cmd/ajilamu/main.go implements both.
type CorrectedRecorder interface {
	// PersistCorrected writes the commit, action, take, charges and timeline
	// snapshot for one corrected source line.
	PersistCorrected(ctx context.Context, req RunRequest, result RunResult, corrected CorrectedRerender) error
}

// CorrectedRerender is the provenance of one corrected-source re-render.
// The route fixes the action and the author, so the ledger row never
// depends on adapter taste.
type CorrectedRerender struct {
	// Action names what the commit changed. It is always text_corrected.
	Action string
	// Author names who caused the change. It is always manual_ui.
	Author string
	// Segment numbers the corrected line.
	Segment int
}

// Rerender failure sentences. A failure never carries internal detail.
const (
	rerenderUnavailable  = "Line re-rendering is unavailable."
	rerenderNoLine       = "No rendered line exists for this project."
	rerenderNoLanguage   = "This project has no target language."
	rerenderBadSegment   = "The segment id must be a whole number."
	rerenderBadBody      = "The request body is not valid JSON."
	rerenderEmptyText    = "The corrected text cannot be empty."
	rerenderEmptySource  = "The corrected source line cannot be empty."
	rerenderBothTexts    = "Send a corrected source line or a corrected target line, not both."
	rerenderBadSpeaker   = "That speaker has no voice in this language."
	rerenderReadFailed   = "Could not read the project ledger."
	rerenderRenderFailed = "The line could not be re-rendered."
	rerenderRecordFailed = "The new take could not be recorded."
)

// rerenderBody is the optional JSON body of the re-render route.
// A nil field keeps the stored value, so an empty body re-renders the line.
type rerenderBody struct {
	// Language overrides the project's stored target language code.
	Language *string `json:"language,omitempty"`
	// Text is a corrected target line. It is authoritative for every
	// attempt, so the loop never translates the source to second-guess it.
	Text *string `json:"text,omitempty"`
	// SourceText is a corrected source line. It replaces the stored
	// transcript and makes the fit loop translate it, because no
	// authoritative target text exists yet.
	SourceText *string `json:"source_text,omitempty"`
	// Speaker reassigns the line to another voice owner.
	Speaker *string `json:"speaker,omitempty"`
}

// RerenderResponse is the 201 body of the line re-render route.
// It names the new take, its price, and the commit that records it.
type RerenderResponse struct {
	// SegmentID numbers the re-rendered line.
	SegmentID int `json:"segment_id"`
	// Language is the resolved target language tag.
	Language string `json:"language"`
	// CommitID names the commit the new take was recorded under.
	CommitID string `json:"commit_id"`
	// TotalNanodollars is the exact price of the new take.
	TotalNanodollars cost.Price `json:"total_nanodollars"`
	// Sentence describes the result in one line of prose.
	Sentence string `json:"sentence"`
	// Text is the spoken target text of the new take.
	Text string `json:"text"`
	// SourceText echoes the corrected source line when the request carried one.
	SourceText string `json:"source_text,omitempty"`
	// Take names the new take.
	Take RerenderTake `json:"take"`
}

// RerenderTake names one re-rendered take and what it cost.
type RerenderTake struct {
	// TakeID identifies the recorded take.
	TakeID string `json:"take_id"`
	// File is the take audio path.
	File string `json:"file"`
	// Name is the take file name without its directory.
	Name string `json:"name"`
	// Attempt orders the take within the line.
	Attempt int `json:"attempt"`
	// Voice names the voice profile that rendered the take.
	Voice string `json:"voice"`
	// Repair names the strategy that produced the file.
	Repair string `json:"repair"`
	// RepairDetail explains an atempo ratio or a rewrite.
	RepairDetail string `json:"repair_detail,omitempty"`
	// Flagged reports that no attempt fit the slot.
	Flagged bool `json:"flagged"`
	// Fit measures the take against its slot.
	Fit Fit `json:"fit"`
	// Charges itemizes what the new take cost.
	Charges []Charge `json:"charges"`
}

// RerenderHandler serves POST /api/dubs/{id}/lines/{segment}/rerender.
//
// It re-runs one dialogue line through the fit loop, saves the result as a new
// take beside the previous ones, and records the take, its charges, and one
// commit. The body is optional and carries a corrected target text, a corrected
// source text, or a speaker. A corrected source re-translates and records the
// text_corrected action. The route never overwrites an existing take file.
func RerenderHandler(renderer LineRenderer, recorder RunRecorder, history HistoryReader, storageDir string, active RunActive, logger *slog.Logger) http.Handler {
	// One re-render at a time keeps two requests from choosing the same new
	// take file. The runner serializes the fit loop for the same reason.
	var takeNumberGate sync.Mutex
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		takeNumberGate.Lock()
		defer takeNumberGate.Unlock()
		if renderer == nil || recorder == nil || history == nil {
			writeHistoryFailure(w, http.StatusServiceUnavailable, rerenderUnavailable)
			return
		}
		dubID := strings.TrimSpace(r.PathValue("id"))
		if !safeProjectID(dubID) {
			writeHistoryFailure(w, http.StatusNotFound, workspaceNotFound)
			return
		}
		segmentID, err := strconv.Atoi(strings.TrimSpace(r.PathValue("segment")))
		if err != nil || segmentID < 0 {
			writeHistoryFailure(w, http.StatusBadRequest, rerenderBadSegment)
			return
		}
		body, err := decodeRerenderBody(r)
		if err != nil {
			writeHistoryFailure(w, http.StatusBadRequest, rerenderBadBody)
			return
		}

		sourceLanguage, language := projectLanguage(storageDir, dubID)
		if body.Language != nil {
			language = strings.TrimSpace(*body.Language)
		}
		if language == "" {
			writeHistoryFailure(w, http.StatusBadRequest, rerenderNoLanguage)
			return
		}
		tag := resolveRunTag(language)
		if err := tts.ValidateLanguage(tag); err != nil {
			writeHistoryFailure(w, http.StatusBadRequest, runBadLanguage)
			return
		}
		if active != nil && active(dubID) {
			writeHistoryFailure(w, http.StatusConflict, runActive)
			return
		}

		// The ledger keys a track by the exact language string the run
		// recorded. The bundled fixture records `ml`, so read under the
		// caller's code first and fall back to the catalog tag.
		entry, err := headLine(r.Context(), history, dubID, language, segmentID)
		if err != nil {
			logHistoryFailure(logger, "read the head line", err)
			writeHistoryFailure(w, http.StatusInternalServerError, rerenderReadFailed)
			return
		}
		if entry == nil && tag != language {
			entry, err = headLine(r.Context(), history, dubID, tag, segmentID)
			if err != nil {
				logHistoryFailure(logger, "read the head line", err)
				writeHistoryFailure(w, http.StatusInternalServerError, rerenderReadFailed)
				return
			}
		}
		if entry == nil {
			writeHistoryFailure(w, http.StatusNotFound, rerenderNoLine)
			return
		}

		segment := types.Segment{
			ID:      segmentID,
			StartMs: entry.StartMs,
			EndMs:   entry.EndMs,
			Text:    entry.SourceText,
			Speaker: types.Speaker{Name: entry.Speaker},
			Emotion: entry.Emotion,
		}
		text := entry.Text
		if body.Text != nil {
			if body.SourceText != nil {
				writeHistoryFailure(w, http.StatusBadRequest, rerenderBothTexts)
				return
			}
			text = strings.TrimSpace(*body.Text)
			if text == "" {
				writeHistoryFailure(w, http.StatusBadRequest, rerenderEmptyText)
				return
			}
		}
		if body.SourceText != nil {
			corrected := strings.TrimSpace(*body.SourceText)
			if corrected == "" {
				writeHistoryFailure(w, http.StatusBadRequest, rerenderEmptySource)
				return
			}
			// The corrected source replaces the stored transcript. An empty
			// target text asks the fit loop to translate it on attempt one.
			segment.Text = corrected
			text = ""
		}
		if body.Speaker != nil {
			speaker := strings.TrimSpace(*body.Speaker)
			if _, err := tts.Assign(types.Speaker{Name: speaker}, tag); err != nil {
				writeHistoryFailure(w, http.StatusBadRequest, rerenderBadSpeaker)
				return
			}
			segment.Speaker.Name = speaker
		}
		// The source path needs a recorder that names the text_corrected
		// action. Check the seam before any model runs.
		correctedRecorder, _ := recorder.(CorrectedRecorder)
		if body.SourceText != nil && correctedRecorder == nil {
			writeHistoryFailure(w, http.StatusServiceUnavailable, rerenderUnavailable)
			return
		}

		workDir := runWorkDirFor(storageDir, dubID, tag)
		tryNumber := nextTakeNumber(workDir, segmentID)
		takeFile := filepath.Join(workDir, fmt.Sprintf("seg_%d_try%d.wav", segmentID, tryNumber))

		rendered, err := renderer.RenderLine(r.Context(), LineRenderRequest{
			DubID:          dubID,
			Language:       tag,
			SourceLanguage: sourceLanguage,
			Segment:        segment,
			Text:           text,
			WorkDir:        workDir,
			TakeFile:       takeFile,
		})
		if err != nil {
			logHistoryFailure(logger, "render the line", err)
			writeHistoryFailure(w, http.StatusBadGateway, rerenderRenderFailed)
			return
		}

		record := RunRequest{DubID: dubID, Language: tag, WorkDir: workDir}
		result := rerenderResult(segmentID, segment, rendered)
		mintRunIdentity(record, &result)
		var recordErr error
		if body.SourceText != nil {
			recordErr = correctedRecorder.PersistCorrected(r.Context(), record, result, CorrectedRerender{
				Action:  ActionTextCorrected,
				Author:  AuthorManualUI,
				Segment: segmentID,
			})
		} else {
			recordErr = recorder.Persist(r.Context(), record, result)
		}
		if recordErr != nil {
			logHistoryFailure(logger, "record the new take", recordErr)
			writeHistoryFailure(w, http.StatusInternalServerError, rerenderRecordFailed)
			return
		}

		correctedSource := ""
		if body.SourceText != nil {
			correctedSource = segment.Text
		}
		writeHistoryJSON(w, http.StatusCreated, rerenderResponseBody(segmentID, tag, correctedSource, result, rendered))
	})
}

// decodeRerenderBody reads the optional JSON body. An empty body is valid and
// leaves every field nil, so the stored line re-renders unchanged.
func decodeRerenderBody(r *http.Request) (rerenderBody, error) {
	if r.Body == nil {
		return rerenderBody{}, nil
	}
	decoder := json.NewDecoder(io.LimitReader(r.Body, 64<<10))
	var body rerenderBody
	if err := decoder.Decode(&body); err != nil {
		if errors.Is(err, io.EOF) {
			return rerenderBody{}, nil
		}
		return rerenderBody{}, err
	}
	return body, nil
}

// headLine reads one line from the newest timeline snapshot of one language.
// It reports a nil entry when the project holds no commit or no such line.
func headLine(ctx context.Context, history HistoryReader, dubID, language string, segmentID int) (*TimelineEntry, error) {
	commits, err := history.ListCommits(ctx, dubID)
	if err != nil {
		return nil, err
	}
	if len(commits) == 0 {
		return nil, nil
	}
	entries, err := history.TimelineAt(ctx, dubID, language, commits[len(commits)-1].CommitID)
	if err != nil {
		return nil, err
	}
	for i := range entries {
		if entries[i].SegmentIndex == segmentID {
			return &entries[i], nil
		}
	}
	return nil, nil
}

// projectLanguage reads the source and target language codes the upload
// record stores. An empty storage directory or a missing record returns
// two empty codes.
func projectLanguage(storageDir, dubID string) (string, string) {
	if storageDir == "" || !safeProjectID(dubID) {
		return "", ""
	}
	payload, err := os.ReadFile(filepath.Join(storageDir, dubID, uploadRecordName))
	if err != nil {
		return "", ""
	}
	var record uploadRecord
	if err := json.Unmarshal(payload, &record); err != nil {
		return "", ""
	}
	return strings.TrimSpace(record.SourceLanguage), strings.TrimSpace(record.Language)
}

// nextTakeNumber returns the first try number that names no existing take.
// It scans the segment's take files, so a re-render never overwrites one.
func nextTakeNumber(workDir string, segmentID int) int {
	next := 1
	pattern := filepath.Join(workDir, fmt.Sprintf("seg_%d_try*.wav", segmentID))
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return next
	}
	for _, match := range matches {
		if number := takeNumberFromName(match); number >= next {
			next = number + 1
		}
	}
	return next
}

// takeNumberFromName reads the try number from a take file name.
// It returns zero when the name carries no try number.
func takeNumberFromName(name string) int {
	base := strings.TrimSuffix(filepath.Base(name), filepath.Ext(name))
	base = strings.TrimSuffix(base, "_stretched")
	at := strings.LastIndex(base, "_try")
	if at < 0 {
		return 0
	}
	number, err := strconv.Atoi(base[at+len("_try"):])
	if err != nil || number < 1 {
		return 0
	}
	return number
}

// rerenderResult maps one rendered line onto the ledger result. The recorded
// attempt is the take file's try number, so the ledger and the file agree.
func rerenderResult(segmentID int, segment types.Segment, rendered LineRenderResult) RunResult {
	take := rendered.Take
	if number := takeNumberFromName(take.File); number > 0 {
		take.Attempt = number
	}
	result := RunResult{
		TotalCost: rendered.Total,
		Takes: []RunTake{{
			Segment:      segment,
			Take:         take,
			Voice:        rendered.Voice,
			Repair:       rendered.Repair,
			RepairDetail: rendered.RepairDetail,
			Charges:      rendered.Charges,
			Peaks:        rendered.Peaks,
		}},
		Timeline: []RunSegmentState{{
			SegmentIndex: segmentID,
			StartMs:      segment.StartMs,
			EndMs:        segment.EndMs,
			Speaker:      segment.Speaker.Name,
			Emotion:      segment.Emotion,
			SourceText:   segment.Text,
			Text:         rendered.Text,
		}},
	}
	if rendered.Flagged {
		result.FlaggedSegments = []int{segmentID}
	}
	return result
}

// rerenderResponseBody builds the 201 body from the recorded result.
// correctedSource echoes the corrected source line, or stays empty.
func rerenderResponseBody(segmentID int, language, correctedSource string, result RunResult, rendered LineRenderResult) RerenderResponse {
	take := result.Takes[0]
	name := filepath.Base(take.Take.File)
	sentence := fmt.Sprintf("Line %d re-rendered as take %s.", segmentID, name)
	if rendered.Flagged {
		sentence = fmt.Sprintf("Line %d re-rendered as take %s and needs review.", segmentID, name)
	}
	return RerenderResponse{
		SegmentID:        segmentID,
		Language:         language,
		CommitID:         result.CommitID,
		TotalNanodollars: result.TotalCost,
		Sentence:         sentence,
		Text:             rendered.Text,
		SourceText:       correctedSource,
		Take: RerenderTake{
			TakeID:       take.TakeID,
			File:         take.Take.File,
			Name:         name,
			Attempt:      take.Take.Attempt,
			Voice:        take.Voice,
			Repair:       repairKind(rendered.Repair),
			RepairDetail: take.RepairDetail,
			Flagged:      rendered.Flagged,
			Fit:          wireFit(take.Take.Fit),
			Charges:      wireCharges(take.Charges, name),
		},
	}
}

// repairKind maps a fit repair onto the wire name.
func repairKind(repair types.Repair) string {
	switch repair {
	case types.RepairAtempo:
		return RepairAtempo
	case types.RepairRewrite:
		return RepairRewrite
	case types.RepairManual:
		return RepairManual
	default:
		return RepairNone
	}
}

// wireFit maps a measured fit onto the wire shape.
func wireFit(fit types.Fit) Fit {
	slot := fit.Slot.Milliseconds()
	measured := fit.Measured.Milliseconds()
	return Fit{
		SlotMs:     slot,
		MeasuredMs: measured,
		DeltaMs:    measured - slot,
		State:      FitState(slot, measured),
	}
}

// wireCharges maps itemized charges onto the wire shape.
func wireCharges(charges []cost.Charge, takeFile string) []Charge {
	out := make([]Charge, 0, len(charges))
	for _, charge := range charges {
		segmentID := charge.TakeID
		out = append(out, Charge{
			Kind:                 charge.Kind.String(),
			SegmentID:            &segmentID,
			TakeFile:             takeFile,
			Units:                int64(charge.Units),
			UnitPriceNanodollars: charge.UnitPrice,
			TotalNanodollars:     charge.Total(),
		})
	}
	return out
}
