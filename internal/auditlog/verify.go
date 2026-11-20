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
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.verifyLocked()
}

func (s *Store) verifyLocked() (int, error) {
	head := ""
	verified := 0
	paths, err := s.paths()
	if err != nil {
		return 0, err
	}
	for _, name := range paths {
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
	if len(s.head) > 0 && head != hex.EncodeToString(s.head) {
		return verified, ErrTampered
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
// Malformed and oversized crash debris is skipped without resetting the chain.
// A subsequent linked record must still name the last verified record, so damage
// to an actual predecessor cannot hide an edited or removed record in the middle.
func verifyFile(f *os.File, carry string) (int, string, error) {
	reader := bufio.NewReaderSize(f, maxLineBytes+1)
	previous, head, count := "", "", 0
	chained := false
	for {
		line, err := readAuditLine(reader)
		var record Event
		if len(line) > 0 && json.Unmarshal(line, &record) == nil {
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
	paths, err := s.paths()
	if err != nil {
		return false, err
	}
	for _, name := range paths {
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
	reader := bufio.NewReaderSize(f, maxLineBytes+1)
	for {
		line, err := readAuditLine(reader)
		var record Event
		if len(line) > 0 && json.Unmarshal(line, &record) == nil {
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

// readAuditLine bounds allocations while consuming the entirety of an oversized
// line. Its final fragment must never be mistaken for a standalone JSON record.
func readAuditLine(reader *bufio.Reader) ([]byte, error) {
	line, err := reader.ReadSlice('\n')
	if errors.Is(err, bufio.ErrBufferFull) {
		for errors.Is(err, bufio.ErrBufferFull) {
			_, err = reader.ReadSlice('\n')
		}
		return nil, err
	}
	line = bytes.TrimSuffix(line, []byte("\n"))
	if len(line) > maxLineBytes {
		return nil, err
	}
	return line, err
}

// openExisting opens a history file for reading without creating or following
// anything unexpected.
func openExisting(name string) (*os.File, error) {
	return openPrivate(name, false)
}
