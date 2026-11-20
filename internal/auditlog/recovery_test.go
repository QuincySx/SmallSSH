package auditlog

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRecoveredAuditTailRetainsHeadAcrossReopen(t *testing.T) {
	for _, tc := range []struct {
		name   string
		tail   []byte
		trimLF bool
	}{
		{name: "missing newline", trimLF: true},
		{name: "partial JSON", tail: []byte(`{"event":"unfinished`)},
		{name: "malformed line", tail: []byte("not-json\n")},
		{name: "oversized partial line", tail: bytes.Repeat([]byte{'x'}, maxLineBytes*3)},
		{name: "oversized complete line", tail: append(bytes.Repeat([]byte{'x'}, maxLineBytes*3), '\n')},
	} {
		for _, rotate := range []bool{false, true} {
			name := tc.name
			if rotate {
				name += " with rotation"
			}
			t.Run(name, func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "audit.jsonl")
				s, err := Open(path)
				if err != nil {
					t.Fatal(err)
				}
				appendEvent(t, s, 0)
				if err := s.Close(); err != nil {
					t.Fatal(err)
				}
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if tc.trimLF {
					data = bytes.TrimSuffix(data, []byte{'\n'})
				}
				data = append(data, tc.tail...)
				if err := os.WriteFile(path, data, 0o600); err != nil {
					t.Fatal(err)
				}
				s, err = Open(path)
				if err != nil {
					t.Fatal(err)
				}
				if rotate {
					// Force the recovered tail into the archive on the next append.
					s.maxBytes = int64(len(data) + 1)
				}
				appendEvent(t, s, 1)
				s.maxBytes = MaxFileBytes
				appendEvent(t, s, 2)
				anchor := s.Head()
				if n, err := s.Verify(); err != nil || n != 3 {
					t.Fatalf("Verify after recovery = %d, %v; want 3 records", n, err)
				}
				if err := s.Close(); err != nil {
					t.Fatal(err)
				}
				s, err = Open(path)
				if err != nil {
					t.Fatalf("reopen after recovered appends: %v", err)
				}
				defer s.Close()
				if s.Head() != anchor {
					t.Fatal("reopen lost the recovered chain head")
				}
				if found, err := s.Contains(anchor); err != nil || !found {
					t.Fatalf("Contains recovered anchor = %v, %v", found, err)
				}
				appendEvent(t, s, 3)
				if n, err := s.Verify(); err != nil || n != 4 {
					t.Fatalf("Verify after another append = %d, %v; want 4", n, err)
				}
			})
		}
	}
}

func TestMalformedMiddleRecordCannotResetAuditChain(t *testing.T) {
	for _, debris := range [][]byte{[]byte("not-json"), bytes.Repeat([]byte{'x'}, maxLineBytes*3)} {
		path := filepath.Join(t.TempDir(), "audit.jsonl")
		s, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		for n := 0; n < 4; n++ {
			appendEvent(t, s, n)
		}
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
		lines := readLines(t, path)
		lines[2] = debris
		if err := os.WriteFile(path, append(bytes.Join(lines, []byte{'\n'}), '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
		if opened, err := Open(path); !errors.Is(err, ErrTampered) {
			if opened != nil {
				opened.Close()
			}
			t.Fatalf("accepted malformed predecessor in linked history: %v", err)
		}
	}
}

func TestMaximumSizeAuditRecordRemainsVerifiable(t *testing.T) {
	value := event(0)
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	// requestId already contains one byte; expanding it fills the exact limit.
	value.RequestID = string(bytes.Repeat([]byte{'x'}, maxLineBytes-len(encoded)+1))
	s, path := openTest(t)
	if err := s.Append(value); err != nil {
		t.Fatal(err)
	}
	anchor := s.Head()
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if n, err := s.Verify(); err != nil || n != 1 {
		t.Fatalf("Verify maximum-size record = %d, %v", n, err)
	}
	if found, err := s.Contains(anchor); err != nil || !found {
		t.Fatalf("Contains maximum-size record = %v, %v", found, err)
	}
}

func TestMissingTailNewlineIsIncludedInRotationBudget(t *testing.T) {
	s, path := openTest(t)
	appendEvent(t, s, 0)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data = bytes.TrimSuffix(data, []byte{'\n'})
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	next := event(1)
	next.Prev = s.Head()
	encoded, err := json.Marshal(next)
	if err != nil {
		t.Fatal(err)
	}
	// This would fit exactly without counting the repaired separator.
	s.maxBytes = int64(len(data) + len(encoded) + 1)
	appendEvent(t, s, 1)
	if _, err := os.Stat(path + ".1"); err != nil {
		t.Fatalf("missing-newline boundary did not rotate: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() > s.maxBytes {
		t.Fatalf("current file grew past retention boundary: %d > %d", info.Size(), s.maxBytes)
	}
	if n, err := s.Verify(); err != nil || n != 2 {
		t.Fatalf("Verify after exact-boundary rotation = %d, %v", n, err)
	}
	page, err := s.History(Query{})
	if err != nil || len(page.Events) == 0 || page.Events[0].RequestID != "1" {
		t.Fatalf("History after exact-boundary rotation = %+v, %v", page, err)
	}
}
