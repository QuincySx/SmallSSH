package auditlog

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAuditAllSegmentsFilterSummaryAndOriginalExport(t *testing.T) {
	s, path := openTest(t)
	s.maxBytes = 1800
	for i := 0; i < 55; i++ {
		v := event(i)
		v.Kind = "exec"
		v.Event = "ended"
		v.Outcome = "success"
		v.Executable = "/Applications/One.app/Contents/MacOS/One"
		v.Command = "uname -a"
		if i%3 == 0 {
			v.Kind = "grant"
			v.Event = "approved"
			v.Executable = "/usr/bin/ssh"
		}
		if err := s.Append(v); err != nil {
			t.Fatal(err)
		}
	}
	head := s.Head()
	s.Close()
	var err error
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	q := Query{Limit: 7, Summary: true, Raw: true, Application: "/usr/bin/ssh"}
	count := 0
	before := ""
	for {
		q.Before = before
		p, err := s.History(q)
		if err != nil {
			t.Fatal(err)
		}
		if count == 0 && (p.Summary == nil || p.Summary.Records != 19 || p.Summary.Authorizations != 19 || p.Summary.Head != head) {
			t.Fatal("summary counted only a page", p.Summary)
		}
		for i, e := range p.Events {
			if e.Executable != q.Application {
				t.Fatal("filter leaked another application")
			}
			h := sha256.Sum256(p.Raw[i])
			if e.RecordHash != hex.EncodeToString(h[:]) {
				t.Fatal("export changed sealed record")
			}
			var original Event
			if json.Unmarshal(p.Raw[i], &original) != nil || original.RecordHash != "" {
				t.Fatal("read metadata changed original")
			}
		}
		count += len(p.Events)
		if p.Next == "" {
			break
		}
		before = p.Next
		q.Summary = false
		if _, err := s.History(Query{Before: before, Search: "other"}); err == nil {
			t.Fatal("cursor accepted changed filters")
		}
	}
	if count != 19 {
		t.Fatal("rotation lost audit rows", count)
	}
	paths, _ := filepath.Glob(path + ".archive-*")
	if len(paths) < 3 {
		t.Fatal("fixture did not cover archival", paths)
	}
}

func TestAuditLongCommandPreservesContentAndFrameBudget(t *testing.T) {
	s, _ := openTest(t)
	command := strings.Repeat("x", 32000)
	for i := 0; i < 45; i++ {
		v := Event{Time: time.Now(), Event: "ended", Kind: "exec", Command: command}
		if err := s.Append(v); err != nil {
			t.Fatal(err)
		}
	}
	p, err := s.History(Query{Limit: 100, Raw: true})
	if err != nil || len(p.Events) == 0 || p.Events[0].Command != command || p.Next == "" {
		t.Fatal("command or pagination lost", err)
	}
	b, _ := json.Marshal(p)
	if len(b) > 1<<20 {
		t.Fatal("audit exceeded IPC frame", len(b))
	}
}

func TestAuditSummaryStaysExactAcrossAppendAndDoesNotAliasReaders(t *testing.T) {
	s, _ := openTest(t)
	q := Query{Summary: true, Search: "tracked"}
	if p, err := s.History(q); err != nil || p.Summary == nil || p.Summary.Records != 0 {
		t.Fatal(p.Summary, err)
	}
	for i, label := range []string{"tracked", "unrelated", "tracked"} {
		v := event(i)
		v.Client = label
		v.Event = "approved"
		v.Executable = "/usr/bin/offline"
		if err := s.Append(v); err != nil {
			t.Fatal(err)
		}
	}
	p, err := s.History(q)
	if err != nil || p.Summary.Records != 2 || p.Summary.Authorizations != 2 || p.Summary.Head != s.Head() {
		t.Fatal("cached counts became stale", p.Summary, err)
	}
	p.Summary.Applications[0].Authorizations = 999
	again, err := s.History(q)
	if err != nil || again.Summary.Applications[0].Authorizations != 2 {
		t.Fatal("reader changed cached audit facts", again.Summary, err)
	}
}
