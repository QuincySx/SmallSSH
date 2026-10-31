package auditlog

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
)

// ErrTampered reports a history that no longer chains: a record was edited or
// removed after it was written.
var ErrTampered = errors.New("审计历史无法验证：记录已被修改或删除")

// Verify checks the archive and the current file in order, then seeds the link
// the next record will carry. Nothing outside this process can detect a complete
// rewrite, but a record removed or edited in the middle breaks the chain.
func (s *Store) Verify() (int, error) {
	head := ""
	verified := 0
	for _, name := range []string{s.path + ".1", s.path} {
		f, err := openExisting(name)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return verified, err
		}
		count, fileHead, err := verifyFile(f, head)
		f.Close()
		if err != nil {
			return verified, err
		}
		verified += count
		if fileHead != "" {
			head = fileHead
		}
	}
	if head != "" {
		digest, err := hex.DecodeString(head)
		if err != nil || len(digest) != sha256.Size {
			return verified, ErrTampered
		}
		s.head = digest
	}
	return verified, nil
}

// verifyFile checks one file and returns how many records it holds plus the
// digest of its last record. A record removed or changed in the middle no longer
// names its predecessor, which is what this comparison catches.
//
// A line that cannot be parsed, or that is larger than a record can be, ends the
// verifiable part: the history reader skips such debris, and one corrupt byte in
// a log must not stop the app from writing. Everything written before it is still
// checked, and a record edited or removed in the middle of a well-formed log is
// still caught.
func verifyFile(f *os.File, carry string) (int, string, error) {
	reader := bufio.NewReaderSize(f, maxLineBytes)
	previous, head, count := "", "", 0
	chained := false
	for {
		line, err := reader.ReadSlice('\n')
		if errors.Is(err, bufio.ErrBufferFull) {
			return count, head, nil // oversized line: stop, as the reader does
		}
		line = bytes.TrimSuffix(line, []byte("\n"))
		if len(line) > 0 {
			var record Event
			if json.Unmarshal(line, &record) != nil {
				return count, head, nil
			}
			switch {
			case record.Prev == "" && !chained:
			case !chained:
				if !namesPredecessor(record.Prev, previous, carry) {
					return count, head, ErrTampered
				}
				chained = true
			case record.Prev != previous:
				return count, head, ErrTampered
			}
			digest := sha256.Sum256(append([]byte(nil), line...))
			head = hex.EncodeToString(digest[:])
			previous = head
			count++
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return count, head, nil
			}
			return count, head, err
		}
	}
}

// namesPredecessor reports whether a record names the record before it: the one
// in the same file, or the last one of the archive when it starts a file.
func namesPredecessor(prev, previousInFile, carry string) bool {
	if previousInFile != "" {
		return prev == previousInFile
	}
	return carry == "" || prev == carry
}

// Head is the digest of the last record, which the next record names as its Prev.
func (s *Store) Head() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.head) == 0 {
		return ""
	}
	return hex.EncodeToString(s.head)
}

// Contains reports whether a record with this digest is still in the retained
// history. The vault anchor is a prefix commitment, not a promise about the
// last line: a crash, or a lock that could not seal the vault, leaves records
// the anchor never saw, and those still start from a record it did see. A
// record that is no longer in either file cannot be checked, which is why a
// rewritten or truncated history is refused instead.
func (s *Store) Contains(want string) (bool, error) {
	if want == "" {
		return false, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, name := range []string{s.path + ".1", s.path} {
		f, err := openExisting(name)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return false, err
		}
		found, err := fileContains(f, want)
		f.Close()
		if err != nil {
			return false, err
		}
		if found {
			return true, nil
		}
	}
	return false, nil
}

// fileContains walks the same records the verifier accepts and compares their
// digests with want.
func fileContains(f *os.File, want string) (bool, error) {
	reader := bufio.NewReaderSize(f, maxLineBytes)
	for {
		line, err := reader.ReadSlice('\n')
		if errors.Is(err, bufio.ErrBufferFull) {
			return false, nil // oversized line: the reader stops here too
		}
		line = bytes.TrimSuffix(line, []byte("\n"))
		if len(line) > 0 {
			var record Event
			if json.Unmarshal(line, &record) != nil {
				return false, nil
			}
			digest := sha256.Sum256(append([]byte(nil), line...))
			if hex.EncodeToString(digest[:]) == want {
				return true, nil
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return false, nil
			}
			return false, err
		}
	}
}

// openExisting opens a history file for reading without creating or following
// anything unexpected.
func openExisting(name string) (*os.File, error) {
	return openPrivate(name, false)
}
