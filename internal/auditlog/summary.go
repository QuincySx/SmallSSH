package auditlog

import (
	"sort"
	"strings"
)

type cachedSummary struct {
	query Query
	value Summary
}

func (s *Summary) add(v Event) {
	if v.Time.IsZero() || v.Event == "" {
		return
	}
	s.Records++
	if s.First.IsZero() || v.Time.Before(s.First) {
		s.First = v.Time
	}
	name := Application(bounded(v))
	index := -1
	for i, a := range s.Applications {
		if a.Application == name {
			index = i
			break
		}
	}
	if index < 0 && len(s.Applications) >= 32 {
		name = "其他应用"
		for i, a := range s.Applications {
			if a.Application == name {
				index = i
				break
			}
		}
	}
	if index < 0 {
		s.Applications = append(s.Applications, ApplicationCount{Application: name})
		index = len(s.Applications) - 1
	}
	a := &s.Applications[index]
	if v.Event == "approved" || v.Event == "ssh_authorized" {
		s.Authorizations++
		a.Authorizations++
	}
	if v.Event == "ended" || v.Event == "system_ended" {
		s.Operations++
		a.Operations++
	}
	if v.Event == "ssh_signed" && v.Outcome == "success" {
		s.Signatures++
		a.Signatures++
	}
}

func (s Summary) snapshot() *Summary {
	s.Applications = append([]ApplicationCount(nil), s.Applications...)
	sort.Slice(s.Applications, func(i, j int) bool {
		return strings.ToLower(s.Applications[i].Application) < strings.ToLower(s.Applications[j].Application)
	})
	return &s
}

// Cached filters remain exact as the sole writer appends. They are bounded in
// number and contain public counters only, not copies of operation bodies.
func (s *Store) updateSummaries(v Event, head string) {
	for key, c := range s.summaries {
		if c.query.matches(v) {
			c.value.add(v)
		}
		c.value.Head = head
		s.summaries[key] = c
	}
}
