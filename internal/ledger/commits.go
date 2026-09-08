package ledger

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"time"
)

const insertCommit = "INSERT INTO commits_raw (commit_id, parent_commit_id, project_id, dub_id, owner_id, branch, language, version_seq, message) FORMAT JSONEachRow"

// Commit is one immutable node in a dub's edit history.
//
// CreatedAt and IngestedAt are omitted deliberately. commits_raw supplies both database
// timestamps, which keeps client clocks out of commit ordering and table partitioning.
type Commit struct {
	CommitID       string `json:"commit_id"`
	ParentCommitID string `json:"parent_commit_id"`
	ProjectID      string `json:"project_id"`
	DubID          string `json:"dub_id"`
	OwnerID        string `json:"owner_id"`
	Branch         string `json:"branch"`
	Language       string `json:"language"`
	VersionSeq     uint64 `json:"version_seq"`
	Message        string `json:"message"`
}

// commitIdentityGate serializes one client and natural commit identity. The registry mutex
// protects only the short map operation. Each identity has its own mutex, so unrelated
// commits can validate and enqueue concurrently.
type commitIdentityGate struct {
	mu    sync.Mutex
	locks map[string]*commitIdentityLock
}

type commitIdentityLock struct {
	mu   sync.Mutex
	refs int
}

var appendCommitGates = commitIdentityGate{locks: make(map[string]*commitIdentityLock)}

func (c *Client) lockCommitIdentity(dubID, commitID string) func() {
	key := fmt.Sprintf("%p:%s:%s", c, dubID, commitID)
	appendCommitGates.mu.Lock()
	lock := appendCommitGates.locks[key]
	if lock == nil {
		lock = &commitIdentityLock{}
		appendCommitGates.locks[key] = lock
	}
	lock.refs++
	appendCommitGates.mu.Unlock()

	lock.mu.Lock()
	return func() {
		lock.mu.Unlock()
		appendCommitGates.mu.Lock()
		lock.refs--
		if lock.refs == 0 {
			delete(appendCommitGates.locks, key)
		}
		appendCommitGates.mu.Unlock()
	}
}

// AppendCommit durably records a new commit without modifying any earlier commit.
// A root has no parent and starts at version one. A child may name any earlier commit,
// allowing a branch to fork from historical state.
func (c *Client) AppendCommit(ctx context.Context, commit Commit) error {
	if err := validateCommit(commit); err != nil {
		return err
	}
	if commit.Branch == "" {
		commit.Branch = "main"
	}
	unlock := c.lockCommitIdentity(commit.DubID, commit.CommitID)
	defer unlock()
	if err := c.validateCommitIdentity(ctx, commit); err != nil {
		return err
	}
	if err := c.validateCommitParent(ctx, commit); err != nil {
		return err
	}
	return c.EnqueueJSON(ctx, insertCommit, commit)
}

// validateCommitIdentity preserves append-only history even though commits_raw uses a
// ReplacingMergeTree natural key. A second insert with the same identity could otherwise
// replace the visible commit during a merge.
func (c *Client) validateCommitIdentity(ctx context.Context, commit Commit) error {
	pending, err := c.pendingCommitIdentity(commit.DubID, commit.CommitID)
	if err != nil {
		return fmt.Errorf("look up pending ledger commit identity: %w", err)
	}
	if pending {
		return fmt.Errorf("ledger commit %q is already queued for dub %q", commit.CommitID, commit.DubID)
	}
	_, found, err := c.commitByID(ctx, commit.DubID, commit.CommitID)
	if err != nil {
		return fmt.Errorf("look up ledger commit identity: %w", err)
	}
	if found {
		return fmt.Errorf("ledger commit %q already exists in dub %q", commit.CommitID, commit.DubID)
	}
	return nil
}

// pendingCommitIdentity consults the durable queue before ClickHouse. A queued commit has
// already claimed its immutable identity, even while a network outage prevents its remote
// row from appearing. Once ClickHouse acknowledges the row, Queue.Flush removes its file.
func (c *Client) pendingCommitIdentity(dubID, commitID string) (bool, error) {
	if c == nil || c.queue == nil {
		return false, errors.New("ledger client is nil")
	}
	c.queue.mu.Lock()
	defer c.queue.mu.Unlock()
	entries, err := c.queue.entries()
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		if entry.Query != insertCommit {
			continue
		}
		var pending Commit
		if err := json.Unmarshal(entry.Body, &pending); err != nil {
			return false, fmt.Errorf("decode queued ledger commit: %w", err)
		}
		if pending.DubID == dubID && pending.CommitID == commitID {
			return true, nil
		}
	}
	return false, nil
}

// validateCommitParent reads the existing parent through the same authenticated ClickHouse
// HTTP protocol used by the durable queue. Each child must name an existing lower-version
// commit. Version sequences therefore decrease on every parent edge, which makes a cycle
// impossible and keeps the history walk finite.
func (c *Client) validateCommitParent(ctx context.Context, commit Commit) error {
	if commit.ParentCommitID == "" {
		return nil
	}
	parent, found, err := c.commitByID(ctx, commit.DubID, commit.ParentCommitID)
	if err != nil {
		return fmt.Errorf("look up ledger commit parent: %w", err)
	}
	if !found {
		return fmt.Errorf("ledger commit parent %q does not exist in dub %q", commit.ParentCommitID, commit.DubID)
	}
	if parent.VersionSeq >= commit.VersionSeq {
		return fmt.Errorf("ledger commit parent %q version %d is not earlier than child version %d", parent.CommitID, parent.VersionSeq, commit.VersionSeq)
	}
	return nil
}

type storedCommit struct {
	CommitID       string `json:"commit_id"`
	ParentCommitID string `json:"parent_commit_id"`
	VersionSeq     uint64 `json:"version_seq"`
}

const selectCommit = "SELECT commit_id, parent_commit_id, version_seq FROM commits_raw FINAL WHERE dub_id = {dub_id:String} AND commit_id = {commit_id:String} FORMAT JSONEachRow"

func (c *Client) commitByID(ctx context.Context, dubID, commitID string) (storedCommit, bool, error) {
	resp, err := c.queryClickHouse(ctx, selectCommit, map[string]string{"dub_id": dubID, "commit_id": commitID})
	if err != nil {
		return storedCommit{}, false, err
	}
	defer resp.Body.Close()
	decoder := json.NewDecoder(resp.Body)
	var parent storedCommit
	if err := decoder.Decode(&parent); errors.Is(err, io.EOF) {
		return storedCommit{}, false, nil
	} else if err != nil {
		return storedCommit{}, false, fmt.Errorf("decode ClickHouse parent lookup: %w", err)
	}
	if parent.CommitID != commitID {
		return storedCommit{}, false, fmt.Errorf("ClickHouse returned unexpected commit %q", parent.CommitID)
	}
	return parent, true, nil
}

// selectCommitHistory lists one dub's commits with their action rows.
//
// The statement joins the commits view to the actions view, so a commit with several
// actions returns once per action row and a commit with no action returns once. The
// collapse runs in Go, where the rule stays testable. created_at leaves as Unix
// milliseconds because commits_raw stores a DateTime64 with no timezone, so a rendered
// string would need the server's zone to interpret.
//
// has_action marks a real action row. An unmatched LEFT JOIN row fills author with the
// Enum8 default agent, so the reader cannot use author as a presence test.
const selectCommitHistory = "SELECT c.commit_id, c.parent_commit_id, c.version_seq, toUnixTimestamp64Milli(c.created_at) AS created_at_ms, if(a.commit_id = '', 0, 1) AS has_action, a.action_type, a.author, a.prompt, toUnixTimestamp64Milli(a.created_at) AS action_created_at_ms, a.event_key FROM (SELECT commit_id, parent_commit_id, version_seq, created_at FROM commits WHERE dub_id = {dub_id:String}) AS c LEFT JOIN (SELECT commit_id, action_type, author, prompt, created_at, event_key FROM actions WHERE dub_id = {dub_id:String}) AS a ON a.commit_id = c.commit_id ORDER BY c.version_seq ASC, c.commit_id ASC FORMAT JSONEachRow"

// CommitHistoryRow is one commit of a dub with the action row that represents it.
//
// Action, Author and Instruction are empty for a commit with no action row.
// CreatedAt is the commit time as RFC 3339 in UTC.
type CommitHistoryRow struct {
	CommitID       string
	ParentCommitID string
	VersionSeq     uint64
	CreatedAt      string
	Action         string
	Author         string
	Instruction    string
}

// commitActionRow is one JSONEachRow line from selectCommitHistory.
// A commit with several action rows appears once per row, and a commit with no action row
// appears once with has_action zero.
type commitActionRow struct {
	CommitID          string `json:"commit_id"`
	ParentCommitID    string `json:"parent_commit_id"`
	VersionSeq        uint64 `json:"version_seq"`
	CreatedAtMs       int64  `json:"created_at_ms"`
	HasAction         uint8  `json:"has_action"`
	ActionType        string `json:"action_type"`
	Author            string `json:"author"`
	Prompt            string `json:"prompt"`
	ActionCreatedAtMs int64  `json:"action_created_at_ms"`
	EventKey          string `json:"event_key"`
}

// ListCommits returns every commit of one dub with its action provenance.
// It orders the result oldest first by version_seq, then by commit_id.
//
// A commit may carry zero, one, or several action rows, while the wire Commit carries one
// action, one author and one instruction. The collapse rule is: a row with a non-empty
// prompt outranks one without, because the wire instruction keeps a command-bar prompt
// verbatim. The newest action_created_at then wins, and the greater event_key breaks a
// timestamp tie. A commit with no action row keeps empty action, author and instruction.
//
// CreatedAt is RFC 3339 in UTC. The statement selects Unix milliseconds because
// commits_raw stores created_at as a DateTime64 with no timezone, so a rendered timestamp
// would need the server's zone to interpret. Epoch milliseconds are absolute, and Go
// formats the instant.
func (c *Client) ListCommits(ctx context.Context, dubID string) ([]CommitHistoryRow, error) {
	if strings.TrimSpace(dubID) == "" {
		return nil, errors.New("ledger commit history dub_id is empty")
	}
	resp, err := c.queryClickHouse(ctx, selectCommitHistory, map[string]string{"dub_id": dubID})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	rows, err := decodeJSONEachRow[commitActionRow](resp.Body)
	if err != nil {
		return nil, fmt.Errorf("decode commit history: %w", err)
	}
	return collapseCommitHistory(rows), nil
}

// collapseCommitHistory groups the joined rows by commit and applies the collapse rule.
// The final sort makes the order independent of the server's row order.
func collapseCommitHistory(rows []commitActionRow) []CommitHistoryRow {
	groups := make(map[string][]commitActionRow, len(rows))
	for _, row := range rows {
		groups[row.CommitID] = append(groups[row.CommitID], row)
	}
	out := make([]CommitHistoryRow, 0, len(groups))
	for _, group := range groups {
		out = append(out, collapseCommitActions(group))
	}
	slices.SortFunc(out, cmpCommitHistoryRow)
	return out
}

func collapseCommitActions(group []commitActionRow) CommitHistoryRow {
	first := group[0]
	row := CommitHistoryRow{
		CommitID:       first.CommitID,
		ParentCommitID: first.ParentCommitID,
		VersionSeq:     first.VersionSeq,
		CreatedAt:      time.UnixMilli(first.CreatedAtMs).UTC().Format(time.RFC3339Nano),
	}
	var winner commitActionRow
	found := false
	for _, candidate := range group {
		if candidate.HasAction == 0 {
			continue
		}
		if !found || commitActionWins(candidate, winner) {
			winner = candidate
			found = true
		}
	}
	if !found {
		return row
	}
	row.Action = winner.ActionType
	row.Author = winner.Author
	row.Instruction = winner.Prompt
	return row
}

// commitActionWins reports whether candidate represents the commit better than current.
// A prompt outranks its absence, then the newest action wins, then the greater event_key.
func commitActionWins(candidate, current commitActionRow) bool {
	if (candidate.Prompt != "") != (current.Prompt != "") {
		return candidate.Prompt != ""
	}
	if candidate.ActionCreatedAtMs != current.ActionCreatedAtMs {
		return candidate.ActionCreatedAtMs > current.ActionCreatedAtMs
	}
	return candidate.EventKey > current.EventKey
}

func cmpCommitHistoryRow(a, b CommitHistoryRow) int {
	if a.VersionSeq != b.VersionSeq {
		return cmp.Compare(a.VersionSeq, b.VersionSeq)
	}
	return cmp.Compare(a.CommitID, b.CommitID)
}

func validateCommit(commit Commit) error {
	for _, field := range []struct {
		name  string
		value string
	}{
		{name: "commit_id", value: commit.CommitID},
		{name: "project_id", value: commit.ProjectID},
		{name: "dub_id", value: commit.DubID},
		{name: "owner_id", value: commit.OwnerID},
	} {
		if strings.TrimSpace(field.value) == "" {
			return errors.New("ledger commit " + field.name + " is empty")
		}
	}
	if commit.VersionSeq == 0 {
		return errors.New("ledger commit version_seq is zero")
	}
	if commit.ParentCommitID == "" && commit.VersionSeq != 1 {
		return errors.New("ledger root commit version_seq must be one")
	}
	if commit.ParentCommitID != "" && commit.VersionSeq == 1 {
		return errors.New("ledger child commit version_seq must exceed one")
	}
	if commit.CommitID == commit.ParentCommitID {
		return errors.New("ledger commit cannot parent itself")
	}
	return nil
}
