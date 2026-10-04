package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/sherlock/internal/message"
)

// readEntries decodes every *.json entry in dir, keyed by file name.
func readEntries(t *testing.T, dir string) map[string]message.Message {
	t.Helper()
	names, err := filepath.Glob(filepath.Join(dir, "*.json"))
	require.NoError(t, err)
	out := map[string]message.Message{}
	for _, name := range names {
		body, err := os.ReadFile(name)
		require.NoError(t, err)
		var msg message.Message
		require.NoError(t, json.Unmarshal(body, &msg), "entry %s", name)
		out[filepath.Base(name)] = msg
	}
	return out
}

// runTool runs the tool's run() once with the given environment and
// call input, the way the platform's runner spawns it.
func runTool(t *testing.T, outbox, inReplyTo, input string) {
	t.Helper()
	runToolAt(t, outbox, inReplyTo, input, time.Now)
}

// runToolAt is runTool with the tool's clock replaced by now.
func runToolAt(t *testing.T, outbox, inReplyTo, input string, now func() time.Time) {
	t.Helper()
	t.Setenv("SHERLOCK_OUTBOX_DIR", outbox)
	t.Setenv("SHERLOCK_AGENT_ID", "agent-7")
	t.Setenv("SHERLOCK_IN_REPLY_TO", inReplyTo)

	stdin, err := os.CreateTemp(t.TempDir(), "stdin")
	require.NoError(t, err)
	_, err = stdin.WriteString(input)
	require.NoError(t, err)
	_, err = stdin.Seek(0, 0)
	require.NoError(t, err)
	stdout, err := os.CreateTemp(t.TempDir(), "stdout")
	require.NoError(t, err)

	origIn, origOut := os.Stdin, os.Stdout
	os.Stdin, os.Stdout = stdin, stdout
	defer func() { os.Stdin, os.Stdout = origIn, origOut }()
	require.NoError(t, run(now))
}

// TestRun_EntryIDIsUniqueAndNamesTheFile writes several entries and
// checks each one's id is distinct and is its file name.
func TestRun_EntryIDIsUniqueAndNamesTheFile(t *testing.T) {
	outbox := t.TempDir()
	const writes = 20
	for range writes {
		runTool(t, outbox, `[]`, `{"channel":"http","target":"alice","content":"hi"}`)
	}

	entries := readEntries(t, outbox)
	require.Len(t, entries, writes, "every write is its own entry")
	ids := map[string]bool{}
	for name, msg := range entries {
		assert.NotEmpty(t, msg.ID)
		assert.Equal(t, msg.ID+".json", name, "the entry's id is its file name")
		ids[msg.ID] = true
	}
	assert.Len(t, ids, writes, "no two entries share an id")
}

// TestRun_EntryRepliesToTheTurnsMessages checks in_reply_to is the
// turn's message IDs as the platform published them, byte for byte — a
// comma inside an ID is part of it, not a separator.
func TestRun_EntryRepliesToTheTurnsMessages(t *testing.T) {
	outbox := t.TempDir()
	runTool(t, outbox, `["msg-1","sender:a,b"]`, `{"channel":"http","target":"alice","content":"hi"}`)

	entries := readEntries(t, outbox)
	require.Len(t, entries, 1)
	for _, msg := range entries {
		assert.Equal(t, []string{"msg-1", "sender:a,b"}, msg.InReplyTo)
		assert.Equal(t, "agent-7", msg.AgentID)
		assert.Equal(t, "hi", msg.Content)
	}
}

// TestWriteEntry_NeverOverwritesAnExistingEntry writes an entry under an
// id that already names one. The write is refused and the existing entry
// is unchanged; the control is that a fresh id in the same directory is
// written.
func TestWriteEntry_NeverOverwritesAnExistingEntry(t *testing.T) {
	outbox := t.TempDir()
	existing := filepath.Join(outbox, "taken.json")
	const first = `{"id":"taken","created_at":"2026-01-02T03:04:05.000000006Z","content":"first"}`
	require.NoError(t, os.WriteFile(existing, []byte(first), 0o600))

	err := writeEntry(outbox, message.Message{ID: "taken", CreatedAt: time.Now().UTC(), Content: "second"})
	require.Error(t, err)
	body, readErr := os.ReadFile(existing)
	require.NoError(t, readErr)
	assert.JSONEq(t, first, string(body), "the existing entry is unchanged, its created_at included")

	require.NoError(t, writeEntry(outbox, message.Message{ID: "fresh", Content: "second"}))
	assert.FileExists(t, filepath.Join(outbox, "fresh.json"))

	leftovers, err := filepath.Glob(filepath.Join(outbox, ".*"))
	require.NoError(t, err)
	assert.Empty(t, leftovers, "the refused write leaves no temporary file behind")
}

// TestWriteEntry_ConcurrentReaderNeverSeesAPartialEntry reads each
// entry's name in a tight loop while that entry is being written, and
// requires that whatever a reader finds under the name is complete. The
// readers know the name before the write starts, so they are already
// spinning on it when a writer that created it before filling it would
// expose the gap.
func TestWriteEntry_ConcurrentReaderNeverSeesAPartialEntry(t *testing.T) {
	outbox := t.TempDir()
	content := strings.Repeat("x", 4<<20)
	const writes = 30

	var next atomic.Pointer[string]
	var done atomic.Bool
	var reads, partial atomic.Int64
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for !done.Load() {
				name := next.Load()
				if name == nil {
					continue
				}
				body, err := os.ReadFile(*name)
				if err != nil {
					continue
				}
				reads.Add(1)
				var msg message.Message
				if json.Unmarshal(body, &msg) != nil || msg.Content != content {
					partial.Add(1)
				}
			}
		}()
	}

	for i := range writes {
		id, err := newEntryID(time.Now())
		require.NoError(t, err, "write %d", i)
		name := filepath.Join(outbox, id+".json")
		next.Store(&name)
		require.NoError(t, writeEntry(outbox, message.Message{ID: id, Content: content}))
	}
	done.Store(true)
	wg.Wait()

	require.Positive(t, reads.Load(), "the readers read entries while they were written")
	assert.Zero(t, partial.Load(), "a reader saw a partial entry")
}

// TestRun_EntryRecordsItsProvenance checks the entry records the
// assignment epoch and generation the platform published, and records
// them empty — unknown — when the process was started without them.
func TestRun_EntryRecordsItsProvenance(t *testing.T) {
	tests := []struct {
		name, epoch, generation string
	}{
		{"published", "epoch:7", "0123456789abcdef0123456789abcdef"},
		{"unknown", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outbox := t.TempDir()
			t.Setenv("SHERLOCK_ASSIGNMENT_EPOCH", tt.epoch)
			t.Setenv("SHERLOCK_GENERATION", tt.generation)
			runTool(t, outbox, `[]`, `{"channel":"http","target":"alice","content":"hi"}`)

			entries := readEntries(t, outbox)
			require.Len(t, entries, 1)
			for name := range entries {
				body, err := os.ReadFile(filepath.Join(outbox, name))
				require.NoError(t, err)
				var raw map[string]any
				require.NoError(t, json.Unmarshal(body, &raw))
				assert.Equal(t, tt.epoch, raw["assignment_epoch"])
				assert.Equal(t, tt.generation, raw["generation"])
			}
		})
	}
}

// TestRun_EntryStatesItsCreationTimeInUTC checks the entry's body carries
// created_at as an RFC 3339 string in UTC with its nanoseconds, though
// the clock reads in another zone.
func TestRun_EntryStatesItsCreationTimeInUTC(t *testing.T) {
	outbox := t.TempDir()
	at := time.Date(2026, 10, 5, 18, 30, 0, 123456789, time.FixedZone("KST", 9*60*60))
	runToolAt(t, outbox, `[]`, `{"channel":"http","target":"alice","content":"hi"}`,
		func() time.Time { return at })

	names, err := filepath.Glob(filepath.Join(outbox, "*.json"))
	require.NoError(t, err)
	require.Len(t, names, 1)
	body, err := os.ReadFile(names[0])
	require.NoError(t, err)
	var raw map[string]any
	require.NoError(t, json.Unmarshal(body, &raw))
	createdAt, ok := raw["created_at"].(string)
	require.True(t, ok, "created_at is a string in the body: %s", body)

	assert.Equal(t, "2026-10-05T09:30:00.123456789Z", createdAt)
	parsed, err := time.Parse(time.RFC3339Nano, createdAt)
	require.NoError(t, err)
	assert.Equal(t, time.UTC, parsed.Location(), "created_at is stated in UTC")
	assert.True(t, parsed.Equal(at))
}

// TestRun_CreationTimeIsTheInstantTheIDWasMintedFrom gives the tool a
// clock that moves on every read, and checks created_at is the same
// instant the entry's ID was minted from rather than a second reading.
func TestRun_CreationTimeIsTheInstantTheIDWasMintedFrom(t *testing.T) {
	outbox := t.TempDir()
	base := time.Date(2026, 10, 5, 9, 30, 0, 0, time.UTC)
	var reads []time.Time
	clock := func() time.Time {
		at := base.Add(time.Duration(len(reads)) * time.Second)
		reads = append(reads, at)
		return at
	}
	runToolAt(t, outbox, `[]`, `{"channel":"http","target":"alice","content":"hi"}`, clock)

	entries := readEntries(t, outbox)
	require.Len(t, entries, 1)
	require.NotEmpty(t, reads)
	minted := reads[0]
	for _, msg := range entries {
		prefix, _, found := strings.Cut(msg.ID, "-")
		require.True(t, found, "id %q has its time prefix", msg.ID)
		assert.Equal(t, fmt.Sprintf("%020d", minted.UnixNano()), prefix, "the id is minted from the first reading")
		assert.True(t, msg.CreatedAt.Equal(minted),
			"created_at %s is the instant the id was minted from, %s", msg.CreatedAt, minted)
	}
}
