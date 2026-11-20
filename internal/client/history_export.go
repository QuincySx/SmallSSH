package client

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
	"time"

	"sshclient/internal/auditlog"
)

// ExportHistory streams the selected history, not merely the visible page.
// JSONL preserves original records/Prev links; its first line describes scope,
// direction and the verified source head. A filtered export is a chain subset.
func (c Client) ExportHistory(ctx context.Context, w io.Writer, q auditlog.Query, format string, owner OwnerProof) (int, error) {
	if format != "JSONL" && format != "CSV" {
		return 0, errors.New("不支持的审计导出格式")
	}
	q.Before = ""
	q.Limit = 30
	q.Raw = format == "JSONL"
	q.Summary = true
	if q.To.IsZero() {
		q.To = time.Now().UTC()
	}
	var csvWriter *csv.Writer
	if format == "CSV" {
		csvWriter = csv.NewWriter(w)
		if err := csvWriter.Write([]string{"time", "event", "outcome", "application", "bundle", "pid", "process_instance", "executable_id", "host", "endpoint", "host_fingerprint", "kind", "command_excerpt", "path", "destination", "object", "excerpt_truncated", "request_id", "parent_id", "authorization", "key_fingerprint", "permission", "expires", "idle_seconds", "reason", "operation_hash", "record_hash", "previous_hash", "client_label", "host_name", "read_root", "write_root", "previous_idle_seconds", "exit_code", "owner_issued", "key_name", "actor_pid", "actor_executable", "actor_instance", "actor_code_id", "skip_medium_review", "medium_review", "previous_medium_review", "parent_generation"}); err != nil {
			return 0, err
		}
	}
	count := 0
	first := true
	for {
		if err := ctx.Err(); err != nil {
			return count, err
		}
		page, err := c.History(ctx, q, owner)
		if err != nil {
			return count, err
		}
		if first && format == "JSONL" {
			header := struct {
				Format    string            `json:"format"`
				Direction string            `json:"direction"`
				Exported  time.Time         `json:"exported"`
				Query     auditlog.Query    `json:"query"`
				Summary   *auditlog.Summary `json:"summary"`
			}{"smallssh-audit/v1", "newest-first", time.Now().UTC(), q, page.Summary}
			if err := json.NewEncoder(w).Encode(header); err != nil {
				return count, err
			}
		}
		for i, e := range page.Events {
			if format == "JSONL" {
				if i >= len(page.Raw) || !json.Valid(page.Raw[i]) {
					return count, errors.New("原始审计记录缺失")
				}
				if _, err := w.Write(append(append([]byte(nil), page.Raw[i]...), '\n')); err != nil {
					return count, err
				}
			} else {
				expires := ""
				if !e.Expires.IsZero() {
					expires = e.Expires.Format(time.RFC3339Nano)
				}
				previousIdle, exitCode := "", ""
				if e.PreviousIdle != nil {
					previousIdle = strconv.Itoa(*e.PreviousIdle)
				}
				if e.ExitCode != nil {
					exitCode = strconv.Itoa(*e.ExitCode)
				}
				mediumReview, previousMediumReview := "", ""
				if e.MediumReview != nil {
					mediumReview = strconv.FormatBool(*e.MediumReview)
				}
				if e.PreviousMediumReview != nil {
					previousMediumReview = strconv.FormatBool(*e.PreviousMediumReview)
				}
				row := []string{e.Time.Format(time.RFC3339Nano), e.Event, e.Outcome, auditlog.Application(e), e.Bundle, strconv.Itoa(e.PeerPID), e.ProcessInstance, e.ExecutableID, e.Host, e.Endpoint, e.HostFingerprint, e.Kind, e.Command, e.Path, e.Destination, e.Object, strconv.FormatBool(e.ExcerptTruncated), e.RequestID, e.ParentID, e.Authorization, e.KeyFingerprint, e.Permission, expires, strconv.Itoa(e.IdleSeconds), e.Reason, e.OperationHash, e.RecordHash, e.Prev, e.Client, e.HostName, e.ReadRoot, e.WriteRoot, previousIdle, exitCode, strconv.FormatBool(e.OwnerIssued), e.KeyName, strconv.Itoa(e.ActorPID), e.ActorExecutable, e.ActorInstance, e.ActorCodeID, strconv.FormatBool(e.SkipMediumReview), mediumReview, previousMediumReview, e.ParentGeneration}
				for j, v := range row {
					if strings.HasPrefix(strings.TrimLeft(v, " \t\r\n"), "=") || strings.HasPrefix(strings.TrimLeft(v, " \t\r\n"), "+") || strings.HasPrefix(strings.TrimLeft(v, " \t\r\n"), "-") || strings.HasPrefix(strings.TrimLeft(v, " \t\r\n"), "@") {
						row[j] = "'" + v
					}
				}
				if err := csvWriter.Write(row); err != nil {
					return count, err
				}
			}
			count++
		}
		if page.Next == "" {
			break
		}
		if page.Next == q.Before {
			return count, errors.New("审计分页未推进")
		}
		q.Before = page.Next
		q.Summary = false
		first = false
	}
	// Recheck after all pages, before the desktop atomically commits the file.
	// Continuations do not repeatedly rescan the entire retained history.
	check := q
	check.Before = ""
	check.Limit = 1
	check.Summary = false
	check.Raw = true
	if _, err := c.History(ctx, check, owner); err != nil {
		return count, err
	}
	if csvWriter != nil {
		csvWriter.Flush()
		if err := csvWriter.Error(); err != nil {
			return count, err
		}
	}
	return count, nil
}
