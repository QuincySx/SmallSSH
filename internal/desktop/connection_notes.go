package desktop

import (
	"context"
	"time"
)

type connectionNotesLoad struct {
	loading      bool
	loadError    string
	loadSequence uint64
	ctx          context.Context
	cancel       context.CancelFunc
	draftEdited  bool
}

func (a *app) startConnectionNotes(id string) {
	ctx, cancel := context.WithCancel(a.operationContext())
	s := &connectionNotesLoad{ctx: ctx, cancel: cancel}
	a.connection.notes = s
	a.loadConnectionNotes(s, id)
}

func (a *app) loadConnectionNotes(s *connectionNotesLoad, id string) {
	if s == nil || a.connection.notes != s || !a.online || a.page != 0 || a.form.ID != id || s.loading {
		return
	}
	s.loadSequence++
	sequence := s.loadSequence
	s.loading, s.loadError = true, ""
	owner, err := a.unlockedOwner()
	if err != nil {
		s.loading = false
		s.loadError = err.Error()
		return
	}
	cli, generation := a.client, a.generation
	go func() {
		ctx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
		defer cancel()
		note, err := cli.ServerNote(ctx, id, owner)
		a.postUI(func() {
			if !a.connectionNotesCurrent(s, id, generation, sequence) {
				return
			}
			s.loading = false
			if err != nil {
				s.loadError = err.Error()
				return
			}
			s.loadError = ""
			manual := a.formNote.Manual
			a.formNoteOriginal = note
			a.formNote = note
			if s.draftEdited {
				a.formNote.Manual = manual
			}
		})
	}()
}

func (a *app) retryConnectionNotes(s *connectionNotesLoad) {
	if s == nil || a.connection.notes != s || !a.online || a.page != 0 || s.loading || s.loadError == "" || a.form.ID == "" {
		return
	}
	a.loadConnectionNotes(s, a.form.ID)
}

func (a *app) connectionNotesCurrent(s *connectionNotesLoad, id string, generation int, sequence uint64) bool {
	return a.online && a.page == 0 && a.connection.notes == s && a.form.ID == id && a.generation == generation && s.loadSequence == sequence && s.ctx.Err() == nil
}

func (a *app) connectionNotesBlocked() bool {
	s := a.connection.notes
	return s != nil && (s.loading || s.loadError != "")
}
