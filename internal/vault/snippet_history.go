package vault

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"sshclient/internal/snippet"
)

func (s *Session) ParameterHistory(id, target, system string, parameter int) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	for _, command := range s.snippets {
		if command.ID != id {
			continue
		}
		definition := snippet.InputDefinition(command, system, parameter)
		for _, history := range command.History {
			if definition != "" && history.Parameter == parameter && history.System == system && history.Target == target && history.Definition == definition {
				return append([]string(nil), history.Values[:min(len(history.Values), snippet.MaxInputHistory)]...)
			}
		}
	}
	return nil
}

// SaveParameterHistory serializes with all other vault writes. It records only
// explicit prompted input, never presets, draft typing or rejected sends.
// A stale editor/run snapshot cannot restore input for changed definitions.
func (s *Session) SaveParameterHistory(file string, command snippet.Command, target, system string, inputs []string, clearParameter int) error {
	if len(target) != 64 || !slices.Contains(snippet.Options(command), system) || len(inputs) > snippet.MaxArgs || clearParameter < -1 || clearParameter >= len(snippet.ArgsFor(command, system)) {
		return errors.New("参数历史范围无效")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrUnlock
	}
	values := make([]snippet.Command, len(s.snippets))
	found := -1
	for i := range s.snippets {
		values[i] = s.snippets[i].Clone()
		if values[i].ID == command.ID {
			found = i
		}
	}
	if found < 0 {
		return errors.New("命令已删除，未保存参数历史")
	}
	saved := &values[found]
	if saved.Body != command.Body || !reflect.DeepEqual(saved.Variants, command.Variants) || !reflect.DeepEqual(saved.Args, command.Args) {
		return errors.New("命令已修改，未保存旧参数历史")
	}
	if clearParameter >= 0 {
		kept := saved.History[:0]
		for _, history := range saved.History {
			if history.Target != target || history.System != system || history.Parameter != clearParameter {
				kept = append(kept, history)
			}
		}
		saved.History = kept
	} else {
		for parameter, arg := range snippet.ArgsFor(*saved, system) {
			definition := snippet.InputDefinition(*saved, system, parameter)
			if definition == "" || parameter >= len(inputs) {
				continue
			}
			value, err := snippet.ParseValue(arg, inputs[parameter])
			if err != nil || strings.TrimSpace(value) == "" || len(value) > 512 || !utf8.ValidString(value) || strings.ContainsAny(value, "\x00\x1b\r\n") {
				continue
			}
			next := snippet.InputHistory{Parameter: parameter, Target: target, System: system, Definition: definition, Updated: time.Now().UnixNano(), Values: []string{value}}
			kept := saved.History[:0]
			for _, history := range saved.History {
				if history.Parameter != parameter || history.Target != target || history.System != system {
					kept = append(kept, history)
					continue
				}
				if history.Definition == definition {
					for _, previous := range history.Values {
						if previous != value && len(next.Values) < snippet.MaxInputHistory {
							next.Values = append(next.Values, previous)
						}
					}
				}
			}
			saved.History = append(kept, next)
		}
	}
	trimParameterHistory(values)
	copy := &Session{header: s.header, key: s.key, routes: s.routes, profiles: s.profiles, snippets: values, notes: s.notes, auditHead: s.auditHead}
	if err := Save(file, copy, false); err != nil {
		return err
	}
	s.snippets = values
	return nil
}

// All commands together retain at most 512 contexts and 1 MiB of input. Oldest
// contexts leave first, so adding another server never grows the vault forever.
func trimParameterHistory(commands []snippet.Command) {
	for {
		count, size, oldestCommand, oldestHistory := 0, 0, -1, -1
		var oldest int64
		for i := range commands {
			for j, history := range commands[i].History {
				count++
				size += len(history.Target) + len(history.Definition) + 128
				for _, value := range history.Values {
					size += len(value)
				}
				if oldestCommand < 0 || history.Updated < oldest {
					oldestCommand, oldestHistory, oldest = i, j, history.Updated
				}
			}
		}
		if count <= 512 && size <= 1<<20 || oldestCommand < 0 {
			return
		}
		history := commands[oldestCommand].History
		commands[oldestCommand].History = append(history[:oldestHistory], history[oldestHistory+1:]...)
	}
}
