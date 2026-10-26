package fileattrs

import (
	"bufio"
	"errors"
	"io"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// Accounts reads only public account names and numeric IDs. Password/hash and
// membership fields never leave the parser. Readers are supplied by the caller.
func Accounts(r io.Reader) ([]Account, error) {
	limited := &io.LimitedReader{R: r, N: 1<<20 + 1}
	scanner := bufio.NewScanner(limited)
	scanner.Buffer(make([]byte, 4096), 64<<10)
	seen := map[string]bool{}
	var out []Account
	for scanner.Scan() {
		parts := strings.SplitN(scanner.Text(), ":", 4)
		if len(parts) < 3 || len(parts[0]) == 0 || len(parts[0]) > 128 || seen[parts[0]] {
			continue
		}
		if strings.IndexFunc(parts[0], func(r rune) bool { return unicode.IsControl(r) || unicode.IsSpace(r) || unicode.In(r, unicode.Cf) }) >= 0 {
			continue
		}
		n, err := strconv.ParseUint(parts[2], 10, 32)
		if err != nil || n == uint64(^uint32(0)) {
			continue
		}
		seen[parts[0]] = true
		if len(out) < 4096 {
			out = append(out, Account{Name: parts[0], ID: uint32(n)})
		}
	}
	if err := scanner.Err(); err != nil {
		return out, err
	}
	if limited.N <= 0 {
		return out, errors.New("账户清单超过读取上限，可使用数字 UID/GID")
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
