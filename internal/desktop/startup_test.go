package desktop

import (
	"testing"

	"github.com/egoist/mygo"
	"sshclient/internal/approval"
	"sshclient/internal/desktopcontrol"
)

func TestBackgroundStartupOnlyExplicitShowRevealsAndRestartResetsIt(t *testing.T) {
	s := newStartup(true)
	shown, stopped := 0, 0
	effects := controlEffects{
		show: func() { shown++ }, quit: func() { stopped++ },
		snapshot: func() desktopcontrol.State { return desktopcontrol.State{Visible: shown > 0, Focused: shown > 0} },
	}
	if s.policy() != mygo.ActivationPolicyAccessory || s.mayPopup() {
		t.Fatal("background process can activate or open approval windows")
	}
	for range 3 {
		r, err := s.control("status", effects)
		if err != nil || r.State.Visible || shown != 0 || r.AfterReply != nil {
			t.Fatal("status revealed window", err)
		}
	}
	if _, err := s.control("unlock", effects); err == nil || shown != 0 {
		t.Fatal("non-window command was accepted")
	}
	r, err := s.control("show", effects)
	if err != nil || !r.State.Visible || shown != 1 || !s.mayPopup() {
		t.Fatal("explicit show did not reveal window", err)
	}
	r, err = s.control("quit", effects)
	if err != nil || stopped != 0 || r.AfterReply == nil {
		t.Fatal("quit did not defer until acknowledgement", err)
	}
	r.AfterReply()
	if stopped != 1 || shown != 1 {
		t.Fatal("quit unexpectedly showed a window")
	}
	next := newStartup(true)
	if next.revealed || next.mayPopup() || next.instance == s.instance || next.activations != 0 {
		t.Fatal("restart restored previous visible state")
	}
	ordinary := newStartup(false)
	if ordinary.policy() != mygo.ActivationPolicyRegular || !ordinary.mayPopup() {
		t.Fatal("ordinary user startup changed")
	}
	if _, err := ordinary.control("quit", effects); err == nil {
		t.Fatal("development control enabled for ordinary app")
	}
}

func TestHiddenDevelopmentNeverCreatesApprovalWindow(t *testing.T) {
	a := &app{startup: newStartup(true)}
	// With no native application initialized, opening any real window would
	// panic. A pending authorization must remain in its queue instead.
	a.showApproval(approval.Request{ID: "pending-development-request", Status: "pending"})
	if a.approvalWin != nil || a.approvalID != "" {
		t.Fatal("hidden development opened an approval popup")
	}
}
