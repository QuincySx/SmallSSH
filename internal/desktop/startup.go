package desktop

import (
	"context"
	"errors"
	"os"

	"github.com/egoist/mygo"
	"sshclient/internal/desktopcontrol"
	"sshclient/internal/vault"
)

type Options struct{ Background bool }

// A new development process always starts hidden, even when its predecessor
// had been explicitly shown. Visibility is never persisted across rebuilds.
type startupState struct {
	background  bool
	revealed    bool
	activations int
	instance    string
}

func newStartup(background bool) startupState {
	return startupState{background: background, revealed: !background, instance: vault.ShortID()}
}

func (s startupState) policy() mygo.ActivationPolicy {
	if s.background {
		return mygo.ActivationPolicyAccessory
	}
	return mygo.ActivationPolicyRegular
}

func (s startupState) mayPopup() bool { return !s.background || s.revealed }

type controlEffects struct {
	show     func()
	snapshot func() desktopcontrol.State
	quit     func()
}

func (s *startupState) control(action string, effects controlEffects) (desktopcontrol.Reply, error) {
	if !s.background {
		return desktopcontrol.Reply{}, errors.New(tr(nil, "开发窗口控制未启用"))
	}
	switch action {
	case "show":
		s.revealed = true
		effects.show()
	case "status", "quit":
	default:
		return desktopcontrol.Reply{}, errors.New(tr(nil, "无效的开发窗口操作"))
	}
	state := effects.snapshot()
	state.PID, state.Instance, state.Ready = os.Getpid(), s.instance, true
	state.BackgroundStart, state.Activations = true, s.activations
	reply := desktopcontrol.Reply{State: state}
	if action == "quit" {
		reply.AfterReply = effects.quit
	}
	return reply, nil
}

func (a *app) handleDesktopControl(ctx context.Context, action string) (desktopcontrol.Reply, error) {
	if !a.startup.background {
		return desktopcontrol.Reply{}, errors.New(tr(nil, "开发窗口控制未启用"))
	}
	type result struct {
		reply desktopcontrol.Reply
		err   error
	}
	response := make(chan result, 1)
	a.postUI(func() {
		if ctx.Err() != nil || a.ctx.Err() != nil {
			response <- result{err: context.Canceled}
			return
		}
		reply, err := a.startup.control(action, controlEffects{
			show: func() {
				mygo.App.SetActivationPolicy(mygo.ActivationPolicyRegular)
				a.win.Show()
				a.win.Focus()
			},
			snapshot: func() desktopcontrol.State {
				return desktopcontrol.State{Visible: a.win.IsVisible(), Focused: a.win.IsFocused()}
			},
			quit: mygo.App.Quit,
		})
		response <- result{reply: reply, err: err}
	})
	select {
	case r := <-response:
		return r.reply, r.err
	case <-ctx.Done():
		return desktopcontrol.Reply{}, ctx.Err()
	case <-a.ctx.Done():
		return desktopcontrol.Reply{}, a.ctx.Err()
	}
}
