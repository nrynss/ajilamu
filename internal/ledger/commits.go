package ledger

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
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
