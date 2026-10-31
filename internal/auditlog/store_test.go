package auditlog

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func event(n int) Event {
	return Event{Time: time.Date(2026, 1, 1, 0, 0, n, 0, time.UTC), Event: "approved", RequestID: fmt.Sprint(n)}
}

func openTest(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, path
}

func appendEvent(t *testing.T, s *Store, n int) {
	t.Helper()
	if err := s.Append(event(n)); err != nil {
		t.Fatal(err)
	}
}

func TestHistoryPersistsLegacyAndPageSnapshotAfterAppendAndReopen(t *testing.T) {
	s, path := openTest(t)
	for i := 0; i < 8; i++ {
		appendEvent(t, s, i)
	}
	first, err := s.History(Query{Limit: 3})
	if err != nil || len(first.Events) != 3 || first.Events[0].RequestID != "7" || first.Next == "" {
		t.Fatal(first, err)
	}
	appendEvent(t, s, 8)
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	next := first.Next
	ids := []string{}
	for next != "" {
		p, e := s.History(Query{Before: next, Limit: 3})
		if e != nil {
			t.Fatal(e)
		}
		for _, v := range p.Events {
			if v.Client != "" || v.HostName != "" {
				t.Fatal("invented legacy identity", v)
			}
			ids = append(ids, v.RequestID)
		}
		next = p.Next
	}
	if fmt.Sprint(ids) != "[4 3 2 1 0]" {
		t.Fatal("append or reopen changed pagination", ids)
	}
	newest, err := s.History(Query{Limit: 1})
	if err != nil || newest.Events[0].RequestID != "8" {
		t.Fatal(newest, err)
	}
}

func TestCursorSurvivesRotationAndFailsAfterRetentionEvictsIt(t *testing.T) {
	s, _ := openTest(t)
	// Records carry a link to the one before them, so measure a linked record
	// instead of a marshalled event.
	probePath := filepath.Join(t.TempDir(), "probe.jsonl")
	probe, err := Open(probePath)
	if err != nil {
		t.Fatal(err)
	}
	probe.Append(event(0))
	one, _ := os.Stat(probePath)
	probe.Append(event(1))
	two, _ := os.Stat(probePath)
	probe.Close()
	s.maxBytes = 4*(two.Size()-one.Size()) + 1
	for i := 0; i < 4; i++ {
		appendEvent(t, s, i)
	}
	first, err := s.History(Query{Limit: 1})
	if err != nil || first.Events[0].RequestID != "3" {
		t.Fatal(first, err)
	}
	appendEvent(t, s, 4) // original current is now the archive
	second, err := s.History(Query{Before: first.Next, Limit: 3})
	if err != nil || fmt.Sprint([]string{second.Events[0].RequestID, second.Events[1].RequestID, second.Events[2].RequestID}) != "[2 1 0]" || second.Next != "" {
		t.Fatal(second, err)
	}
	for i := 5; i < 10; i++ {
		appendEvent(t, s, i)
	}
	if _, err = s.History(Query{Before: first.Next, Limit: 3}); !errors.Is(err, ErrCursor) {
		t.Fatal("evicted cursor silently moved to different records", err)
	}
}

func TestMalformedOversizedAndTruncatedLinesDoNotHideValidHistory(t *testing.T) {
	s, path := openTest(t)
	s.Close()
	good, _ := json.Marshal(event(1))
	data := append(append(append([]byte{}, good...), '\n'), []byte("not-json\n")...)
	data = append(data, bytes.Repeat([]byte{'x'}, maxLineBytes*3)...)
	data = append(data, '\n')
	data = append(data, []byte("{\"event\":\"unfinished")...)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p, err := s.History(Query{})
	if err != nil || len(p.Events) != 1 || p.Events[0].RequestID != "1" {
		t.Fatal(p, err)
	}
	appendEvent(t, s, 2)
	p, err = s.History(Query{})
	if err != nil || len(p.Events) != 2 || p.Events[0].RequestID != "2" || p.Events[1].RequestID != "1" {
		t.Fatal(p, err)
	}
}

func TestRotationAndOversizedLegacyFilesStayBounded(t *testing.T) {
	s, path := openTest(t)
	s.maxBytes = 1024
	for i := 0; i < 100; i++ {
		appendEvent(t, s, i)
	}
	for _, name := range []string{path, path + ".1"} {
		st, err := os.Stat(name)
		if err != nil || st.Size() > s.maxBytes {
			t.Fatal(name, st, err)
		}
	}
	s.Close()
	legacy := bytes.Repeat([]byte{'x'}, MaxFileBytes+100)
	b, _ := json.Marshal(event(999))
	legacy = append(append(legacy, '\n'), append(b, '\n')...)
	if err := os.WriteFile(path, legacy, 0600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	st, _ := os.Stat(path)
	p, err := s.History(Query{})
	if err != nil || st.Size() > MaxFileBytes || len(p.Events) < 1 || p.Events[0].RequestID != "999" {
		t.Fatal(st.Size(), p, err)
	}
}

func TestHistoryBoundsAndMetadataJSONControls(t *testing.T) {
	s, _ := openTest(t)
	for _, q := range []Query{{Limit: -1}, {Limit: MaxPageSize + 1}, {Before: "invalid"}, {Before: string(bytes.Repeat([]byte{'x'}, 2049))}} {
		if _, err := s.History(q); err == nil {
			t.Fatal("accepted invalid query", q.Limit)
		}
	}
	v := event(1)
	v.Client = "unverified\n\x00<script>&label"
	if err := s.Append(v); err != nil {
		t.Fatal(err)
	}
	p, err := s.History(Query{})
	if err != nil || len(p.Events) != 1 || p.Events[0].Client != v.Client {
		t.Fatal(p, err)
	}
	if err := s.Append(Event{Event: string(bytes.Repeat([]byte{'x'}, maxLineBytes+1))}); err == nil {
		t.Fatal("accepted oversized event")
	}
	for i := 2; i < 110; i++ {
		appendEvent(t, s, i)
	}
	p, err = s.History(Query{Limit: MaxPageSize})
	if err != nil || len(p.Events) != MaxPageSize || p.Next == "" {
		t.Fatal("unbounded history page", len(p.Events), err)
	}
}

func TestPrivateFilesRejectSymlinksPermissionsAndSpecialFiles(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permissions")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "generated-fixture")
	if err := os.WriteFile(target, []byte("generated"), 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "audit.jsonl")
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); err == nil {
		t.Fatal("accepted symlink")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); err == nil {
		t.Fatal("accepted public permissions")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); err == nil {
		t.Fatal("accepted directory")
	}
	data, _ := os.ReadFile(target)
	if string(data) != "generated" {
		t.Fatal("followed symlink target")
	}
}

func TestLegacyMetadataCannotOverflowPageRPCFrame(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	var data bytes.Buffer
	encoder := json.NewEncoder(&data)
	encoder.SetEscapeHTML(false)
	v := event(0)
	v.Client = string(bytes.Repeat([]byte{'<'}, 7000))
	for i := 0; i < MaxPageSize; i++ {
		if err := encoder.Encode(v); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(path, data.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p, err := s.History(Query{Limit: MaxPageSize})
	if err != nil || len(p.Events) != MaxPageSize || len(p.Events[0].Client) > 128 {
		t.Fatal("unbounded legacy metadata", err)
	}
	b, err := json.Marshal(p)
	if err != nil || len(b) > 1<<20 {
		t.Fatal("page exceeds local RPC frame", len(b), err)
	}
}

func TestMalformedLineScanBudgetContinuesWithCursor(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	good, _ := json.Marshal(event(1))
	data := append(append(good, '\n'), bytes.Repeat([]byte("{}\n"), maxScannedLines+1)...)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	first, err := s.History(Query{})
	if err != nil || len(first.Events) != 0 || first.Next == "" {
		t.Fatal("malformed scan was not bounded", first, err)
	}
	second, err := s.History(Query{Before: first.Next})
	if err != nil || len(second.Events) != 1 || second.Events[0].RequestID != "1" || second.Next != "" {
		t.Fatal("bounded scan lost earlier valid history", second, err)
	}
}

// Records chain to the one written before them, and editing or dropping one in
// the middle is refused instead of trusted.
func TestHistoryRecordsChainAndTamperIsRefused(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"exec", "get", "put", "shell"} {
		if err = s.Append(Event{Event: "requested", Kind: kind, Host: "fixture"}); err != nil {
			t.Fatal(err)
		}
	}
	if n, err := s.Verify(); err != nil || n != 4 {
		t.Fatalf("Verify = %d, %v; want 4 records", n, err)
	}
	s.Close()

	lines := readLines(t, path)
	if len(lines) != 4 {
		t.Fatalf("wrote %d lines, want 4", len(lines))
	}
	var first Event
	if json.Unmarshal(lines[0], &first) != nil || first.Prev != "" {
		t.Fatal("the first record must start the chain")
	}
	for i := 1; i < len(lines); i++ {
		var record Event
		if json.Unmarshal(lines[i], &record) != nil {
			t.Fatal("unparsable record")
		}
		digest := sha256.Sum256(lines[i-1])
		if record.Prev != hex.EncodeToString(digest[:]) {
			t.Fatalf("record %d does not name its predecessor", i)
		}
	}

	// Editing a record in the middle breaks the chain.
	edited := bytes.Replace(lines[2], []byte(`"put"`), []byte(`"get"`), 1)
	if bytes.Equal(edited, lines[2]) {
		t.Fatal("test did not change the record")
	}
	lines[2] = edited
	if err = os.WriteFile(path, append(bytes.Join(lines, []byte("\n")), '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err = Open(path); !errors.Is(err, ErrTampered) {
		t.Fatalf("Open accepted an edited history: %v", err)
	}

	// So does dropping one.
	lines = readLines(t, path)
	lines[2] = bytes.Replace(lines[2], []byte(`"get"`), []byte(`"put"`), 1)
	dropped := append(append([][]byte{}, lines[:1]...), lines[2:]...)
	if err = os.WriteFile(path, append(bytes.Join(dropped, []byte("\n")), '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err = Open(path); !errors.Is(err, ErrTampered) {
		t.Fatalf("Open accepted a history with a record removed: %v", err)
	}
}

// A history written before records carried a link is accepted, and the next
// record starts chaining from it.
func TestHistoryFromBeforeTheChainStillOpens(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")
	legacy := `{"time":"2026-10-01T00:00:00Z","event":"requested","kind":"exec"}
{"time":"2026-10-01T00:00:01Z","event":"ended","kind":"exec"}
`
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatalf("a pre-chain history was refused: %v", err)
	}
	if err = s.Append(Event{Event: "requested", Kind: "get"}); err != nil {
		t.Fatal(err)
	}
	if n, err := s.Verify(); err != nil || n != 3 {
		t.Fatalf("Verify = %d, %v; want 3 records", n, err)
	}
	s.Close()
}

// Rotation keeps the chain: the first record of the new file names the last
// record of the archive.
func TestHistoryChainSurvivesRotation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s.maxBytes = 600 // force a rotation after a few records
	for i := 0; i < 12; i++ {
		if err = s.Append(Event{Event: "requested", Kind: "exec", Host: fmt.Sprintf("fixture-%d", i)}); err != nil {
			t.Fatal(err)
		}
	}
	if n, err := s.Verify(); err != nil || n < 2 {
		t.Fatalf("Verify = %d, %v after rotation", n, err)
	}
	s.Close()
	if _, err = Open(path); err != nil {
		t.Fatalf("rotation left an unverifiable history: %v", err)
	}
	if _, err = os.Stat(path + ".1"); err != nil {
		t.Fatal("expected a rotated archive")
	}
}

func readLines(t *testing.T, path string) [][]byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	trimmed := bytes.TrimSuffix(b, []byte("\n"))
	if len(trimmed) == 0 {
		return nil
	}
	return bytes.Split(trimmed, []byte("\n"))
}

// The vault anchor is a prefix commitment: a record the vault stored must stay
// findable even after rotation moved it into the archive, and a digest that is
// gone from the retained history must be reported as missing.
func TestAnchorDigestStaysFindableAcrossRotation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s.maxBytes = 600 // rotate every few records
	for i := 0; i < 3; i++ {
		appendEvent(t, s, i)
	}
	anchor := s.Head()
	if anchor == "" {
		t.Fatal("no head after writing records")
	}
	for i := 3; i < 7; i++ {
		appendEvent(t, s, i)
	}
	if _, err = os.Stat(path + ".1"); err != nil {
		t.Fatal("expected a rotated archive")
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if found, err := s.Contains(anchor); err != nil || !found {
		t.Fatalf("Contains(anchored record) = %v, %v; want true", found, err)
	}
	if s.Head() == anchor {
		t.Fatal("the anchor should be a prefix, not the head, after later records")
	}
	missing := "00" + anchor[2:]
	if found, err := s.Contains(missing); err != nil || found {
		t.Fatalf("Contains(unknown digest) = %v, %v; want false", found, err)
	}
	if found, err := s.Contains(""); err != nil || found {
		t.Fatalf("Contains(empty) = %v, %v; want false", found, err)
	}
}
