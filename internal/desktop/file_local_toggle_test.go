package desktop

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestShowLocalFileWindowButton(t *testing.T) {
	a := fileFollowDemo()
	a.ensureFileLayout()
	remote := a.primaryFilePane
	remoteBrowser := remote.browser()
	local := a.localFilePane
	local.browser().dir = "/cached-local"
	local.browser().address = "/cached-local"
	local.browser().cache["/cached-local"] = nil
	tt := ui.NewTester(a.fileArea, 960, 420)
	if err := tt.Click("显示本机文件"); err != nil {
		t.Fatal(err)
	}
	panes := a.fileLayout.panes()
	if len(panes) != 2 || panes[0] != remote || panes[1] != local || local.browser().dir != "/cached-local" || remote.browser() != remoteBrowser {
		t.Fatal("local window did not preserve the cached local and remote panes")
	}
	if err := tt.Click("显示本机文件"); err != nil {
		t.Fatal(err)
	}
	if len(a.fileLayout.panes()) != 2 || a.activeFilePane != local {
		t.Fatal("existing local window was not focused without duplication")
	}
}

func TestShowLocalFileWindowAtPaneLimit(t *testing.T) {
	a := fileFollowDemo()
	a.ensureFileLayout()
	remote := a.primaryFilePane
	for len(a.fileLayout.panes()) < maxFilePanes {
		if a.splitFilePane(remote, false, false) == nil {
			t.Fatal("could not fill pane layout")
		}
	}
	if a.showLocalFileWindow(remote) != nil || len(a.fileLayout.panes()) != maxFilePanes {
		t.Fatal("added a local pane beyond the limit")
	}
	local := a.fileLayout.panes()[maxFilePanes-1]
	local.endpoint = localEndpoint
	a.activeFilePane = remote
	if a.showLocalFileWindow(remote) != local || a.activeFilePane != local || len(a.fileLayout.panes()) != maxFilePanes {
		t.Fatal("did not focus existing local pane at the limit")
	}
}
