// Package ledger writes immutable provenance events to ClickHouse.
package ledger

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// ErrPending reports that an event is durable locally but has not reached ClickHouse.
var ErrPending = errors.New("ledger event remains queued")

// Entry is one JSONEachRow payload waiting to be inserted.
// Query must be the INSERT statement for the payload's target table.
type Entry struct {
	Query string `json:"query"`
	Body  []byte `json:"body"`
}

type queuedEntry struct {
	Entry
	path string
}

// Queue durably holds entries until ClickHouse acknowledges their insertion.
// Files remain in insertion order, so a reconnect replays the same event sequence.
type Queue struct {
	dir       string
	batchSize int

	mu sync.Mutex
}

// OpenQueue opens a local durable queue. A batchSize below one is invalid.
func OpenQueue(dir string, batchSize int) (*Queue, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, errors.New("ledger queue directory is empty")
	}
	if batchSize < 1 {
		return nil, fmt.Errorf("ledger queue batch size = %d, want at least 1", batchSize)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create ledger queue directory: %w", err)
	}
	return &Queue{dir: dir, batchSize: batchSize}, nil
}

// Enqueue stores an entry before attempting network delivery.
func (q *Queue) Enqueue(entry Entry) error {
	if strings.TrimSpace(entry.Query) == "" {
		return errors.New("ledger queue entry query is empty")
	}
	if len(entry.Body) == 0 {
		return errors.New("ledger queue entry body is empty")
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	sequence, err := q.nextSequence()
	if err != nil {
		return err
	}
	payload, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("encode ledger queue entry: %w", err)
	}
	name := fmt.Sprintf("%020d.json", sequence)
	path := filepath.Join(q.dir, name)
	if err := writeFileAtomic(path, payload); err != nil {
		return fmt.Errorf("persist ledger queue entry: %w", err)
	}
	return syncDirectory(q.dir)
}

// Flush sends pending entries in same-query batches. It removes a batch only after the
// sender returns nil. Failed entries therefore survive reconnects and process restarts.
func (q *Queue) Flush(ctx context.Context, sender func(context.Context, []Entry) error) error {
	if sender == nil {
		return errors.New("ledger queue sender is nil")
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	entries, err := q.entries()
	if err != nil {
		return err
	}
	for start := 0; start < len(entries); {
		if err := ctx.Err(); err != nil {
			return err
		}
		end := start + 1
		for end < len(entries) && end-start < q.batchSize && entries[end].Query == entries[start].Query {
			end++
		}
		batch := make([]Entry, end-start)
		for i := range batch {
			batch[i] = entries[start+i].Entry
		}
		if err := sender(ctx, batch); err != nil {
			return fmt.Errorf("%w: send %d ledger entries: %v", ErrPending, len(batch), err)
		}
		for _, entry := range entries[start:end] {
			if err := os.Remove(entry.path); err != nil {
				return fmt.Errorf("remove acknowledged ledger entry: %w", err)
			}
		}
		if err := syncDirectory(q.dir); err != nil {
			return err
		}
		start = end
	}
	return nil
}

// Len reports the number of locally durable entries awaiting acknowledgement.
func (q *Queue) Len() (int, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	entries, err := q.entries()
	return len(entries), err
}

func (q *Queue) entries() ([]queuedEntry, error) {
	files, err := os.ReadDir(q.dir)
	if err != nil {
		return nil, fmt.Errorf("read ledger queue: %w", err)
	}
	entries := make([]queuedEntry, 0, len(files))
	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".json") {
			continue
		}
		path := filepath.Join(q.dir, file.Name())
		body, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read ledger queue entry: %w", err)
		}
		var entry Entry
		if err := json.Unmarshal(body, &entry); err != nil {
			return nil, fmt.Errorf("decode ledger queue entry %s: %w", file.Name(), err)
		}
		if strings.TrimSpace(entry.Query) == "" || len(entry.Body) == 0 {
			return nil, fmt.Errorf("invalid ledger queue entry %s", file.Name())
		}
		entries = append(entries, queuedEntry{Entry: entry, path: path})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].path < entries[j].path })
	return entries, nil
}

func (q *Queue) nextSequence() (uint64, error) {
	files, err := os.ReadDir(q.dir)
	if err != nil {
		return 0, fmt.Errorf("read ledger queue sequence: %w", err)
	}
	var highest uint64
	for _, file := range files {
		name := strings.TrimSuffix(file.Name(), ".json")
		if name == file.Name() {
			continue
		}
		sequence, err := strconv.ParseUint(name, 10, 64)
		if err != nil {
			continue
		}
		if sequence > highest {
			highest = sequence
		}
	}
	if highest == ^uint64(0) {
		return 0, errors.New("ledger queue sequence exhausted")
	}
	return highest + 1, nil
}

func writeFileAtomic(path string, body []byte) error {
	temp, err := os.CreateTemp(filepath.Dir(path), ".pending-*")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if err := temp.Chmod(0o600); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(body); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempName, path)
}

func syncDirectory(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("open ledger queue directory: %w", err)
	}
	defer f.Close()
	if err := f.Sync(); err != nil && !errors.Is(err, fs.ErrInvalid) {
		return fmt.Errorf("sync ledger queue directory: %w", err)
	}
	return nil
}
