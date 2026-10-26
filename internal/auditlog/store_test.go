package auditlog

import (
	"bytes"
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
	b, _ := json.Marshal(event(0))
	s.maxBytes = int64(4*(len(b)+1) + 1)
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
