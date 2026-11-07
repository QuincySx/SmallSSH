package desktop

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"sshclient/internal/connectcfg"
	"sshclient/internal/vault"
)

func waitConnectionNotesLoadedForSaveTest(t *testing.T, h *connectionFlowHarness) {
	t.Helper()
	h.wait(func() bool {
		notes := h.a.connection.notes
		return notes != nil && !notes.loading
	})
	notes := h.a.connection.notes
	if notes == nil {
		t.Fatal("connection notes loader disappeared")
	}
	if notes.loadError != "" {
		t.Fatalf("connection notes load error = %q", notes.loadError)
	}
}

func summaryByIDForSaveTest(t *testing.T, summaries []vault.Summary, id string) vault.Summary {
	t.Helper()
	for _, summary := range summaries {
		if summary.ID == id {
			return summary
		}
	}
	t.Fatalf("summary for connection %q not found", id)
	return vault.Summary{}
}

func openConnectionNotesForSaveTest(t *testing.T, h *connectionFlowHarness, id string) {
	t.Helper()
	h.a.connections = h.server.Vault.Summaries()
	h.a.selected = id
	h.a.page = 1
	h.a.editConnection()
	waitConnectionNotesLoadedForSaveTest(t, h)
}

func TestConnectionNotesPendingRetryRestoredManualOverwritesUncertainRemoteNote(t *testing.T) {
	h := newConnectionFlowHarness(t)
	t.Cleanup(h.a.resetConnectionEditor)

	p, originalNote := seedConnectionNotesRPC(t, h)
	openConnectionNotesForSaveTest(t, h, p.ID)
	h.a.connection.connect = false
	h.ui.Frame()
	if _, ok := h.ui.Find("保存连接"); !ok {
		t.Fatalf("save label with connect disabled: %v", h.ui.Texts())
	}
	authsBefore := h.fixture.Auths.Load()

	const savedName = "profile saved before uncertain note"
	const attemptedManual = "manual draft whose note save fails"
	h.input("名称", savedName)
	h.input("服务器备注", attemptedManual)
	h.a.formNote.Details = strings.Repeat("x", 16385)
	h.ui.Frame()

	h.a.saveProfile()
	h.wait(func() bool {
		if h.a.connection.pendingNote != nil {
			return true
		}
		if h.a.connection.phase != "" {
			return false
		}
		if h.stored(p.ID).Name == savedName {
			return true
		}
		return h.a.connection.errors["note"] != "" ||
			h.a.connection.errors["form"] != "" ||
			h.a.connection.errors["key"] != ""
	})
	pending := h.a.connection.pendingNote
	if pending == nil {
		t.Fatalf("save did not reach pending note state: phase=%q errors=%v", h.a.connection.phase, h.a.connection.errors)
	}
	if pending.connect || pending.id != p.ID || pending.summary.Name != savedName {
		t.Fatalf("pending note = %#v, want saved profile %q without connecting", pending, p.ID)
	}
	if !reflect.DeepEqual(h.a.formNoteOriginal, originalNote) {
		t.Fatalf("UI original = %#v, want %#v", h.a.formNoteOriginal, originalNote)
	}
	if h.a.formNote.Manual != attemptedManual {
		t.Fatalf("UI manual after note failure = %q, want %q", h.a.formNote.Manual, attemptedManual)
	}
	if _, ok := h.ui.Find("重试保存备注"); !ok {
		t.Fatalf("retry label missing: %v", h.ui.Texts())
	}

	profileAfterFirstSave := h.stored(p.ID)
	if profileAfterFirstSave.Name != savedName {
		t.Fatalf("stored name = %q, want %q", profileAfterFirstSave.Name, savedName)
	}

	markerNote := originalNote
	markerNote.Manual = "uncertain outcome marker"
	owner, err := h.a.unlockedOwner()
	if err != nil {
		t.Fatalf("unlock owner for independent note write: %v", err)
	}
	if err := h.a.client.SaveServerNote(h.ctx, p.ID, markerNote, owner); err != nil {
		t.Fatalf("independently save uncertain remote note: %v", err)
	}
	remoteMarker, err := h.server.Vault.ServerNote(p.ID)
	if err != nil {
		t.Fatalf("read independently saved note: %v", err)
	}
	if !reflect.DeepEqual(remoteMarker, markerNote) {
		t.Fatalf("remote note = %#v, want independent marker %#v", remoteMarker, markerNote)
	}
	if reflect.DeepEqual(remoteMarker, originalNote) {
		t.Fatal("independent note write did not change the remote baseline")
	}
	if !reflect.DeepEqual(h.a.formNoteOriginal, originalNote) {
		t.Fatalf("independent remote write changed UI original: %#v", h.a.formNoteOriginal)
	}

	h.input("服务器备注", originalNote.Manual)
	h.a.formNote.Details = originalNote.Details
	h.ui.Frame()
	if h.a.formNote.Manual != originalNote.Manual {
		t.Fatalf("restored UI manual = %q, want %q", h.a.formNote.Manual, originalNote.Manual)
	}
	if _, ok := h.ui.Find("服务器备注输入"); !ok || !h.ui.Focused("服务器备注输入") {
		t.Fatalf("restored manual input is not visible and focused: %v", h.ui.Texts())
	}
	retryNote := h.a.formNote
	if !reflect.DeepEqual(retryNote, originalNote) {
		t.Fatalf("retry note = %#v, want restored original %#v", retryNote, originalNote)
	}

	h.click("重试保存备注")
	h.wait(func() bool {
		if h.a.connection.errors["note"] != "" {
			return true
		}
		if h.a.connection.pendingNote != nil || h.a.connection.phase != "" {
			return false
		}
		note, err := h.server.Vault.ServerNote(p.ID)
		return err == nil && reflect.DeepEqual(note, retryNote) && h.a.page == 1
	})
	if noteError := h.a.connection.errors["note"]; noteError != "" {
		t.Fatalf("note retry error = %q", noteError)
	}
	if h.a.connection.pendingNote != nil || h.a.connection.phase != "" {
		t.Fatalf("retry remained pending: phase=%q pending=%#v", h.a.connection.phase, h.a.connection.pendingNote)
	}
	if h.a.page != 1 {
		t.Fatalf("page = %d after note retry, want workspace page 1", h.a.page)
	}

	storedRetryNote, err := h.server.Vault.ServerNote(p.ID)
	if err != nil {
		t.Fatalf("read retried server note: %v", err)
	}
	if !reflect.DeepEqual(storedRetryNote, retryNote) {
		t.Fatalf("retried server note = %#v, want %#v", storedRetryNote, retryNote)
	}
	if storedRetryNote.Manual == markerNote.Manual {
		t.Fatalf("retry left independent marker in server note: %#v", storedRetryNote)
	}
	if !reflect.DeepEqual(h.a.formNoteOriginal, retryNote) || !reflect.DeepEqual(h.a.formNote, retryNote) {
		t.Fatalf("retried note baseline = %#v draft = %#v, want %#v", h.a.formNoteOriginal, h.a.formNote, retryNote)
	}
	if finalProfile := h.stored(p.ID); !reflect.DeepEqual(finalProfile, profileAfterFirstSave) {
		t.Fatalf("note retry changed saved profile: %#v, want %#v", finalProfile, profileAfterFirstSave)
	}
	if auths := h.fixture.Auths.Load(); auths != authsBefore {
		t.Fatalf("note retry performed SSH authentication: auths=%d before=%d", auths, authsBefore)
	}
}

func TestConnectionNotesSavePendingRetryDoesNotReplayProfile(t *testing.T) {
	h := newConnectionFlowHarness(t)
	t.Cleanup(h.a.resetConnectionEditor)

	p, originalNote := seedConnectionNotesRPC(t, h)
	authsBefore := h.fixture.Auths.Load()
	h.a.connections = h.server.Vault.Summaries()
	h.a.selected = p.ID
	h.a.page = 1
	h.a.editConnection()
	waitConnectionNotesLoadedForSaveTest(t, h)

	h.a.connection.connect = false
	const savedName = "profile saved before note failure"
	h.input("名称", savedName)
	h.a.connection.replace = true
	h.ui.Frame()
	h.input("服务器密码", p.Password)

	proxy := connectcfg.Proxy{
		Mode:     "socks5",
		Address:  "127.0.0.1:1080",
		User:     "fixture",
		Password: "synthetic proxy secret",
	}
	h.a.form.Network.Proxy = proxy
	h.a.connection.replaceProxy = true
	const privateKey = "synthetic private key"
	const keyPassphrase = "synthetic key passphrase"
	const hardwarePIN = "synthetic hardware pin"
	const keyFile = "/synthetic/fixture-key"
	h.a.form.PrivateKey = privateKey
	h.a.form.KeyPassphrase = keyPassphrase
	h.a.form.Hardware.PIN = hardwarePIN
	h.a.keyFile = keyFile

	const newManual = "manual note after profile success"
	h.input("服务器备注", newManual)
	h.a.formNote.Details = strings.Repeat("x", 16385)
	if h.a.formNote.Manual != newManual {
		t.Fatalf("note draft manual = %q, want %q", h.a.formNote.Manual, newManual)
	}
	if h.a.formNote.Discovered != originalNote.Discovered ||
		!h.a.formNote.CheckedAt.Equal(originalNote.CheckedAt) ||
		!reflect.DeepEqual(h.a.formNote.Target, originalNote.Target) {
		t.Fatalf("note snapshot changed before save: %#v", h.a.formNote)
	}

	h.click("保存连接")
	h.wait(func() bool {
		if h.a.connection.pendingNote != nil {
			return true
		}
		if h.a.connection.phase != "" {
			return false
		}
		if h.stored(p.ID).Name == savedName {
			return true
		}
		return h.a.connection.errors["note"] != "" ||
			h.a.connection.errors["form"] != "" ||
			h.a.connection.errors["key"] != ""
	})
	pending := h.a.connection.pendingNote
	if pending == nil {
		t.Fatalf("save did not reach pending note state: phase=%q errors=%v", h.a.connection.phase, h.a.connection.errors)
	}
	if pending.connect {
		t.Fatal("pending note unexpectedly requested a connection")
	}
	if pending.summary.Name != savedName || pending.id != p.ID {
		t.Fatalf("pending note = %#v, want saved profile %q", pending, p.ID)
	}

	storedAfterProfile := h.stored(p.ID)
	if storedAfterProfile.Name != savedName {
		t.Fatalf("stored name = %q, want %q", storedAfterProfile.Name, savedName)
	}
	if storedAfterProfile.Password != p.Password {
		t.Fatal("stored password was not committed by the profile save")
	}
	if storedAfterProfile.Network.Proxy.Password != proxy.Password {
		t.Fatal("stored proxy password was not committed by the profile save")
	}

	wantSummary := connectionSummary(storedAfterProfile)
	gotSummary := summaryByIDForSaveTest(t, h.a.connections, p.ID)
	if !reflect.DeepEqual(gotSummary, wantSummary) {
		t.Fatalf("connection summary = %#v, want %#v", gotSummary, wantSummary)
	}
	if h.a.details == nil || !reflect.DeepEqual(*h.a.details, wantSummary) {
		t.Fatalf("details summary = %#v, want %#v", h.a.details, wantSummary)
	}
	secrets := []string{p.Password, proxy.Password, privateKey, keyPassphrase, hardwarePIN, keyFile, storedAfterProfile.PrivateKey, storedAfterProfile.KeyPassphrase, storedAfterProfile.Hardware.PIN}
	publicState := fmt.Sprintf("%#v %#v", gotSummary, *h.a.details)
	for _, secret := range secrets {
		if secret != "" && strings.Contains(publicState, secret) {
			t.Fatal("public summary or details contains a secret")
		}
	}

	serverNote, err := h.server.Vault.ServerNote(p.ID)
	if err != nil {
		t.Fatalf("read note after failed save: %v", err)
	}
	if !reflect.DeepEqual(serverNote, originalNote) {
		t.Fatalf("server note changed after rejected note save: %#v", serverNote)
	}
	if h.a.formNote.Manual != newManual {
		t.Fatalf("UI manual draft = %q, want %q", h.a.formNote.Manual, newManual)
	}
	if !reflect.DeepEqual(h.a.formNoteOriginal, originalNote) {
		t.Fatalf("UI original note = %#v, want %#v", h.a.formNoteOriginal, originalNote)
	}
	if h.a.connection.errors["note"] == "" {
		t.Fatal("note rejection was not reported")
	}
	if !h.ui.HasText("连接已保存，备注未保存") {
		t.Fatal("pending note status is not visible")
	}
	if h.a.form.Password != "" ||
		h.a.form.PrivateKey != "" ||
		h.a.form.KeyPassphrase != "" ||
		h.a.form.Network.Proxy.Password != "" ||
		h.a.form.Hardware.PIN != "" ||
		h.a.keyFile != "" ||
		h.a.ownerPass != "" {
		t.Fatal("input secrets were not cleared after the profile save")
	}
	if h.a.connection.replace || h.a.connection.replaceProxy {
		t.Fatal("credential replacement flags were not cleared")
	}
	if h.fixture.Auths.Load() != authsBefore {
		t.Fatal("profile save with pending note started SSH")
	}

	pendingBefore := *h.a.connection.pendingNote
	savedDraftName := h.a.form.Name
	const focusTransitionManual = "pending note focus transition"
	h.input("服务器备注", focusTransitionManual)
	_ = h.ui.Click("名称输入")
	h.ui.Type("must not edit connection name")
	if h.a.form.Name != savedDraftName {
		t.Fatalf("disabled name input changed draft name to %q", h.a.form.Name)
	}
	_ = h.ui.Click("保存后连接")
	h.ui.Frame()
	if h.a.connection.connect {
		t.Fatal("disabled save-and-connect control changed connection request")
	}
	if !reflect.DeepEqual(*h.a.connection.pendingNote, pendingBefore) {
		t.Fatal("pending retry payload changed with draft controls")
	}
	if got := summaryByIDForSaveTest(t, h.a.connections, p.ID); got.Name != savedName {
		t.Fatalf("pending controls changed summary name to %q", got.Name)
	}

	const transientManual = "pending note is still editable"
	h.input("服务器备注", transientManual)
	if h.a.formNote.Manual != transientManual {
		t.Fatal("manual note could not be edited while pending note exists")
	}
	h.input("服务器备注", newManual)
	if h.a.formNote.Manual != newManual {
		t.Fatalf("restored note draft = %q, want %q", h.a.formNote.Manual, newManual)
	}

	base := h.a.form
	if base.Network.Proxy.Mode != proxy.Mode ||
		base.Network.Proxy.Address != proxy.Address ||
		base.Network.Proxy.User != proxy.User {
		t.Fatalf("current form proxy = %#v, want saved SOCKS5 proxy", base.Network.Proxy)
	}
	const canaryName = "independent profile canary"
	canaryPolicy := base.Policy
	canaryPolicy.Disabled = !canaryPolicy.Disabled
	canaryChange := vault.ProfileChange{
		Profile:             base,
		KeepCredential:      true,
		KeepProxyCredential: true,
	}
	canaryChange.Profile.Name = canaryName
	canaryChange.Profile.Policy = canaryPolicy
	cli := h.a.client
	owner := h.server.Vault
	if err := cli.SaveConnection(h.ctx, canaryChange, owner); err != nil {
		t.Fatalf("independent canary profile save: %v", err)
	}
	canaryStored := h.stored(p.ID)
	if canaryStored.Name != canaryName {
		t.Fatalf("canary stored name = %q, want %q", canaryStored.Name, canaryName)
	}
	if canaryStored.Policy.Disabled != canaryPolicy.Disabled {
		t.Fatalf("canary policy disabled = %v, want %v", canaryStored.Policy.Disabled, canaryPolicy.Disabled)
	}
	if canaryStored.Password != storedAfterProfile.Password ||
		canaryStored.Network.Proxy.Password != storedAfterProfile.Network.Proxy.Password {
		t.Fatal("independent canary save replaced retained credentials")
	}
	canaryPolicyDisabled := canaryStored.Policy.Disabled
	canaryPassword := canaryStored.Password
	canaryProxyPassword := canaryStored.Network.Proxy.Password
	if h.a.connection.pendingNote == nil || !reflect.DeepEqual(*h.a.connection.pendingNote, pendingBefore) {
		t.Fatal("independent save replaced the pending retry payload")
	}

	const retryDetails = "valid retry details marker"
	h.a.formNote.Details = retryDetails
	retryNote := h.a.formNote
	h.click("重试保存备注")
	h.wait(func() bool {
		if h.a.connection.phase != "" {
			return false
		}
		note, err := h.server.Vault.ServerNote(p.ID)
		if err != nil {
			return h.a.connection.errors["note"] != ""
		}
		return (reflect.DeepEqual(note, retryNote) && h.a.connection.pendingNote == nil && h.a.page == 1) ||
			h.a.connection.errors["note"] != ""
	})
	if h.a.connection.errors["note"] != "" {
		t.Fatalf("retry note error = %q", h.a.connection.errors["note"])
	}
	if h.a.connection.pendingNote != nil {
		t.Fatal("pending note remained after retry")
	}
	if h.a.page != 1 {
		t.Fatalf("page = %d after successful retry, want workspace", h.a.page)
	}
	serverNote, err = h.server.Vault.ServerNote(p.ID)
	if err != nil {
		t.Fatalf("read retried note: %v", err)
	}
	if !reflect.DeepEqual(serverNote, retryNote) {
		t.Fatalf("retried server note = %#v, want %#v", serverNote, retryNote)
	}
	if serverNote.Manual != newManual {
		t.Fatalf("retried manual = %q, want %q", serverNote.Manual, newManual)
	}
	if serverNote.Discovered != originalNote.Discovered ||
		!serverNote.CheckedAt.Equal(originalNote.CheckedAt) ||
		!reflect.DeepEqual(serverNote.Target, originalNote.Target) {
		t.Fatalf("retry changed preserved note snapshot: %#v", serverNote)
	}
	finalStored := h.stored(p.ID)
	if finalStored.Name != canaryName || finalStored.Policy.Disabled != canaryPolicyDisabled {
		t.Fatalf("retry replayed the saved profile: %#v", finalStored)
	}
	if finalStored.Password != canaryPassword || finalStored.Network.Proxy.Password != canaryProxyPassword {
		t.Fatal("retry replaced retained credentials")
	}
	if h.fixture.Auths.Load() != authsBefore {
		t.Fatal("retry with connect=false started SSH")
	}
}

func TestConnectionNotesPendingCloseRequiresConfirmation(t *testing.T) {
	h := newConnectionFlowHarness(t)
	t.Cleanup(h.a.resetConnectionEditor)

	p, originalNote := seedConnectionNotesRPC(t, h)
	authsBefore := h.fixture.Auths.Load()
	openConnectionNotesForSaveTest(t, h, p.ID)

	h.a.connection.connect = false
	const savedName = "profile saved before pending close"
	const draftManual = "pending manual draft must survive close prompt"
	longDetails := strings.Repeat("注", 16385)
	h.input("名称", savedName)
	h.input("服务器备注", draftManual)
	h.a.formNote.Details = longDetails
	h.ui.Frame()

	h.click("保存连接")
	h.wait(func() bool {
		if h.a.connection.pendingNote != nil {
			return true
		}
		if h.a.connection.phase != "" {
			return false
		}
		return h.a.connection.errors["note"] != "" ||
			h.a.connection.errors["form"] != "" ||
			h.a.connection.errors["key"] != ""
	})
	pending := h.a.connection.pendingNote
	if pending == nil {
		t.Fatalf("save did not reach pending note state: phase=%q errors=%v", h.a.connection.phase, h.a.connection.errors)
	}
	if pending.connect {
		t.Fatal("pending note unexpectedly requested a connection")
	}
	if pending.id != p.ID || pending.summary.Name != savedName {
		t.Fatalf("pending note = %#v, want saved profile %q", pending, p.ID)
	}
	if h.a.page != 0 || h.a.formNote.Manual != draftManual || h.a.formNote.Details != longDetails {
		t.Fatalf("pending draft state = page %d note %#v", h.a.page, h.a.formNote)
	}
	if h.a.connection.errors["note"] == "" || !h.ui.HasText("连接已保存，备注未保存") {
		t.Fatal("pending note rejection is not visible")
	}
	if h.fixture.Auths.Load() != authsBefore {
		t.Fatal("save with connect=false started SSH before the note retry")
	}

	savedProfile := h.stored(p.ID)
	if savedProfile.Name != savedName || savedProfile.Password != p.Password {
		t.Fatalf("stored profile after pending save = %#v", savedProfile)
	}
	serverNote, err := h.server.Vault.ServerNote(p.ID)
	if err != nil {
		t.Fatalf("read note after pending save: %v", err)
	}
	if !reflect.DeepEqual(serverNote, originalNote) {
		t.Fatalf("server note changed before retry: %#v", serverNote)
	}

	pendingBefore := *pending
	draftBefore := h.a.formNote
	editorUI := h.ui
	closeLabel := "取消"
	if h.ui.HasText("放弃备注并关闭") {
		closeLabel = "放弃备注并关闭"
	}
	if !h.ui.HasText(closeLabel) {
		t.Fatalf("connection close control unavailable: %v", h.ui.Texts())
	}
	h.click(closeLabel)
	h.ui.Frame()
	closePrompt := h.a.prompt
	if !closePrompt.open || closePrompt.confirm == nil || closePrompt.title == "" || closePrompt.message == "" || closePrompt.accept == "" {
		t.Fatalf("close confirmation prompt = %#v", closePrompt)
	}
	if h.a.page != 0 || h.a.connection.pendingNote == nil || !reflect.DeepEqual(*h.a.connection.pendingNote, pendingBefore) || !reflect.DeepEqual(h.a.formNote, draftBefore) {
		t.Fatalf("opening close confirmation changed editor: page=%d pending=%#v note=%#v", h.a.page, h.a.connection.pendingNote, h.a.formNote)
	}

	h.ui = ui.NewTester(h.a.promptView, 820, 650)
	h.ui.Frame()
	h.click("取消")
	h.ui.Frame()
	if h.a.prompt.open {
		t.Fatal("cancel did not dismiss the close confirmation")
	}
	if h.a.page != 0 || h.a.connection.pendingNote == nil || !reflect.DeepEqual(*h.a.connection.pendingNote, pendingBefore) || !reflect.DeepEqual(h.a.formNote, draftBefore) {
		t.Fatalf("dismissing close confirmation changed editor: page=%d pending=%#v note=%#v", h.a.page, h.a.connection.pendingNote, h.a.formNote)
	}

	h.ui = editorUI
	h.ui.Frame()
	h.click(closeLabel)
	h.ui.Frame()
	closePrompt = h.a.prompt
	if !closePrompt.open || closePrompt.confirm == nil || closePrompt.title == "" || closePrompt.message == "" || closePrompt.accept == "" {
		t.Fatalf("second close confirmation prompt = %#v", closePrompt)
	}
	if closePrompt.generation != h.a.generation || !h.a.online {
		t.Fatalf("close confirmation generation = %d, want %d", closePrompt.generation, h.a.generation)
	}
	h.ui = ui.NewTester(h.a.promptView, 820, 650)
	h.ui.Frame()
	h.click(h.a.prompt.accept)
	h.ui.Frame()
	h.ui = editorUI
	h.ui.Frame()

	if h.a.page != 1 || h.a.connection.pendingNote != nil || h.a.connection.notes != nil {
		t.Fatalf("confirmed close did not leave the editor: page=%d pending=%#v notes=%#v", h.a.page, h.a.connection.pendingNote, h.a.connection.notes)
	}
	if h.a.form.Password != "" || h.a.form.PrivateKey != "" || h.a.form.KeyPassphrase != "" || h.a.form.Network.Proxy.Password != "" || h.a.form.Hardware.PIN != "" || h.a.keyFile != "" || h.a.ownerPass != "" {
		t.Fatalf("confirmed close left input secrets: password=%q privateKey=%q keyPassphrase=%q proxyPassword=%q pin=%q keyFile=%q ownerPass=%q", h.a.form.Password, h.a.form.PrivateKey, h.a.form.KeyPassphrase, h.a.form.Network.Proxy.Password, h.a.form.Hardware.PIN, h.a.keyFile, h.a.ownerPass)
	}
	finalProfile := h.stored(p.ID)
	if finalProfile.Name != savedName || finalProfile.Password != p.Password {
		t.Fatalf("confirmed close changed the saved profile: %#v", finalProfile)
	}
	finalNote, err := h.server.Vault.ServerNote(p.ID)
	if err != nil {
		t.Fatalf("read note after confirmed close: %v", err)
	}
	if !reflect.DeepEqual(finalNote, originalNote) {
		t.Fatalf("confirmed close saved the pending note draft: %#v", finalNote)
	}
	if h.fixture.Auths.Load() != authsBefore {
		t.Fatal("confirmed close started SSH")
	}
}

func TestConnectionNotesRetryAfterSSHFailureUnfreezesEditor(t *testing.T) {
	h := newConnectionFlowHarness(t)
	t.Cleanup(h.a.resetConnectionEditor)

	p, originalNote := seedConnectionNotesRPC(t, h)
	authsBefore := h.fixture.Auths.Load()
	openConnectionNotesForSaveTest(t, h, p.ID)

	const savedName = "profile saved before failed SSH"
	const wrongPassword = "definitely wrong fixture password"
	const manual = "manual saved with the retried note"
	longDetails := strings.Repeat("界", 16385)
	h.input("名称", savedName)
	h.input("服务器备注", manual)
	h.a.formNote.Details = longDetails
	h.a.connection.replace = true
	h.ui.Frame()
	h.input("服务器密码", wrongPassword)
	h.a.connection.connect = true
	h.ui.Frame()

	h.click("保存并连接")
	h.wait(func() bool {
		if h.a.connection.pendingNote != nil {
			return true
		}
		if h.a.connection.phase != "" {
			return false
		}
		return h.a.connection.errors["note"] != "" ||
			h.a.connection.errors["form"] != "" ||
			h.a.connection.errors["key"] != ""
	})
	pending := h.a.connection.pendingNote
	if pending == nil {
		t.Fatalf("save did not reach pending note state: phase=%q errors=%v", h.a.connection.phase, h.a.connection.errors)
	}
	if !pending.connect {
		t.Fatal("pending note lost the requested SSH connection")
	}
	if pending.id != p.ID || pending.summary.Name != savedName {
		t.Fatalf("pending note = %#v, want saved profile %q", pending, p.ID)
	}
	if h.a.page != 0 || h.a.connection.errors["note"] == "" || !h.ui.HasText("连接已保存，备注未保存") {
		t.Fatalf("pending note state = page %d errors %v", h.a.page, h.a.connection.errors)
	}
	if h.fixture.Auths.Load() != authsBefore {
		t.Fatal("pending note started SSH before the note retry")
	}

	savedProfile := h.stored(p.ID)
	if savedProfile.Name != savedName || savedProfile.Password != wrongPassword {
		t.Fatalf("stored profile after pending save = %#v", savedProfile)
	}
	savedSummary := connectionSummary(savedProfile)
	if h.a.details == nil || !reflect.DeepEqual(*h.a.details, savedSummary) {
		t.Fatalf("details summary = %#v, want %#v", h.a.details, savedSummary)
	}
	serverNote, err := h.server.Vault.ServerNote(p.ID)
	if err != nil {
		t.Fatalf("read note after pending save: %v", err)
	}
	if !reflect.DeepEqual(serverNote, originalNote) {
		t.Fatalf("server note changed before retry: %#v", serverNote)
	}

	const retryDetails = "valid retried note details"
	oldNoteError := h.a.connection.errors["note"]
	h.a.formNote.Details = retryDetails
	retryNote := h.a.formNote
	h.ui.Frame()
	h.click("重试保存备注")
	h.wait(func() bool {
		if noteError := h.a.connection.errors["note"]; noteError != "" && noteError != oldNoteError {
			return true
		}
		if h.a.connection.pendingNote != nil {
			return false
		}
		note, err := h.server.Vault.ServerNote(p.ID)
		if err != nil || !reflect.DeepEqual(note, retryNote) {
			return false
		}
		if h.a.connection.phase != "" {
			return false
		}
		return h.a.connection.errors["form"] != "" || h.a.connection.errors["trust"] != ""
	})

	if noteError := h.a.connection.errors["note"]; noteError != "" {
		t.Fatalf("note retry error = %q", noteError)
	}
	if h.a.connection.pendingNote != nil || h.a.connection.phase != "" {
		t.Fatalf("retry remained pending: phase=%q pending=%#v", h.a.connection.phase, h.a.connection.pendingNote)
	}
	if h.a.page != 0 {
		t.Fatalf("page = %d after SSH failure, want editable editor", h.a.page)
	}
	failureText := ""
	for key, message := range h.a.connection.errors {
		if key != "note" && message != "" {
			failureText = message
			break
		}
	}
	if failureText == "" {
		t.Fatalf("SSH authentication failure was not reported: %v", h.a.connection.errors)
	}
	if !h.ui.HasText(failureText) || !h.ui.HasText("登录未成功") {
		t.Fatalf("SSH failure is not visible: error=%q texts=%v", failureText, h.ui.Texts())
	}
	if h.fixture.Auths.Load() <= authsBefore {
		t.Fatalf("SSH authentication was not attempted: auths=%d before=%d", h.fixture.Auths.Load(), authsBefore)
	}

	storedRetryNote, err := h.server.Vault.ServerNote(p.ID)
	if err != nil {
		t.Fatalf("read retried note: %v", err)
	}
	if !reflect.DeepEqual(storedRetryNote, retryNote) {
		t.Fatalf("retried server note = %#v, want %#v", storedRetryNote, retryNote)
	}
	if !reflect.DeepEqual(h.a.formNoteOriginal, retryNote) || !reflect.DeepEqual(h.a.formNote, retryNote) {
		t.Fatalf("retried note baseline = %#v draft = %#v, want %#v", h.a.formNoteOriginal, h.a.formNote, retryNote)
	}
	if retryNote.Details != retryDetails || retryNote.Manual != manual {
		t.Fatalf("retried note content = %#v", retryNote)
	}
	profileAfterFailure := h.stored(p.ID)
	if profileAfterFailure.Name != savedName || profileAfterFailure.Password != wrongPassword {
		t.Fatalf("SSH failure changed the saved profile: %#v", profileAfterFailure)
	}
	if h.a.details == nil || !reflect.DeepEqual(*h.a.details, connectionSummary(profileAfterFailure)) {
		t.Fatalf("details summary after SSH failure = %#v", h.a.details)
	}
	if h.a.connection.replace || h.a.form.Password != "" || h.a.form.PrivateKey != "" || h.a.form.KeyPassphrase != "" || h.a.form.Network.Proxy.Password != "" || h.a.form.Hardware.PIN != "" || h.a.keyFile != "" || h.a.ownerPass != "" {
		t.Fatalf("SSH failure left replacement enabled or input secrets: replace=%v form=%#v", h.a.connection.replace, h.a.form)
	}

	const editedAfterFailure = "editable form after SSH failure"
	h.input("名称", editedAfterFailure)
	if h.a.form.Name != editedAfterFailure {
		t.Fatalf("form is not editable after SSH failure: name=%q", h.a.form.Name)
	}

	h.click("更换登录凭据")
	h.ui.Frame()
	if !h.ui.HasText("服务器密码") {
		t.Fatal("replacement password field did not appear")
	}
	h.input("服务器密码", p.Password)
	h.a.connection.connect = false
	h.ui.Frame()
	if !h.ui.HasText("保存连接") {
		t.Fatalf("save control after disabling connect: %v", h.ui.Texts())
	}

	h.click("保存连接")
	h.wait(func() bool {
		if h.a.page == 1 && h.a.connection.pendingNote == nil {
			return true
		}
		if h.a.connection.phase != "" {
			return false
		}
		err := h.a.connection.errors["form"]
		return (err != "" && err != failureText) || h.a.connection.errors["note"] != ""
	})
	if h.a.page != 1 || h.a.connection.pendingNote != nil || h.a.connection.phase != "" {
		t.Fatalf("final save did not close the editor: page=%d phase=%q pending=%#v errors=%v", h.a.page, h.a.connection.phase, h.a.connection.pendingNote, h.a.connection.errors)
	}
	if h.a.connection.errors["note"] != "" || h.a.connection.errors["form"] != "" {
		t.Fatalf("final save errors: %v", h.a.connection.errors)
	}
	finalProfile := h.stored(p.ID)
	if finalProfile.Name != editedAfterFailure || finalProfile.Password != p.Password {
		t.Fatalf("final stored profile = %#v", finalProfile)
	}
	finalNote, err := h.server.Vault.ServerNote(p.ID)
	if err != nil {
		t.Fatalf("read final note: %v", err)
	}
	if !reflect.DeepEqual(finalNote, retryNote) {
		t.Fatalf("final note = %#v, want confirmed snapshot %#v", finalNote, retryNote)
	}
	if h.a.connection.replace || h.a.connection.replaceProxy || h.a.form.Password != "" || h.a.form.PrivateKey != "" || h.a.form.KeyPassphrase != "" || h.a.form.Network.Proxy.Password != "" || h.a.form.Hardware.PIN != "" || h.a.keyFile != "" || h.a.ownerPass != "" {
		t.Fatalf("final save left replacement enabled or input secrets: replace=%v", h.a.connection.replace)
	}
}

func TestConnectionNotesManualSaveAndClearPreserveSnapshot(t *testing.T) {
	h := newConnectionFlowHarness(t)
	t.Cleanup(h.a.resetConnectionEditor)

	p, originalNote := seedConnectionNotesRPC(t, h)
	authsBefore := h.fixture.Auths.Load()
	credentialsBefore := h.stored(p.ID)
	h.a.connections = h.server.Vault.Summaries()
	h.a.selected = p.ID
	h.a.page = 1
	h.a.editConnection()
	waitConnectionNotesLoadedForSaveTest(t, h)

	const firstManual = "first manual save"
	h.input("服务器备注", firstManual)
	h.click("保存连接")
	h.wait(func() bool {
		if h.a.connection.phase != "" {
			return false
		}
		note, err := h.server.Vault.ServerNote(p.ID)
		if err != nil {
			return h.a.connection.errors["note"] != ""
		}
		return (note.Manual == firstManual && h.a.page == 1 && h.a.connection.pendingNote == nil) ||
			h.a.connection.errors["note"] != "" ||
			h.a.connection.errors["form"] != ""
	})
	if h.a.connection.errors["note"] != "" || h.a.connection.errors["form"] != "" {
		t.Fatalf("first note save errors: %v", h.a.connection.errors)
	}
	firstStored, err := h.server.Vault.ServerNote(p.ID)
	if err != nil {
		t.Fatalf("read first saved note: %v", err)
	}
	if firstStored.Manual != firstManual {
		t.Fatalf("first saved manual = %q, want %q", firstStored.Manual, firstManual)
	}
	if firstStored.Discovered != originalNote.Discovered ||
		firstStored.Details != originalNote.Details ||
		!firstStored.CheckedAt.Equal(originalNote.CheckedAt) ||
		!reflect.DeepEqual(firstStored.Target, originalNote.Target) {
		t.Fatalf("first manual save changed snapshot: %#v", firstStored)
	}
	afterFirstProfile := h.stored(p.ID)
	if afterFirstProfile.Password != credentialsBefore.Password ||
		afterFirstProfile.Network.Proxy.Password != credentialsBefore.Network.Proxy.Password {
		t.Fatal("first note save changed retained credentials")
	}
	if h.fixture.Auths.Load() != authsBefore {
		t.Fatal("first note save started SSH")
	}

	h.a.connections = h.server.Vault.Summaries()
	h.a.selected = p.ID
	h.a.page = 1
	h.a.editConnection()
	waitConnectionNotesLoadedForSaveTest(t, h)
	if !reflect.DeepEqual(h.a.formNote, firstStored) {
		t.Fatalf("reloaded note = %#v, want %#v", h.a.formNote, firstStored)
	}

	h.a.formNote.Manual = ""
	h.click("保存连接")
	h.wait(func() bool {
		if h.a.connection.phase != "" {
			return false
		}
		note, err := h.server.Vault.ServerNote(p.ID)
		if err != nil {
			return h.a.connection.errors["note"] != ""
		}
		return (note.Manual == "" && h.a.page == 1 && h.a.connection.pendingNote == nil) ||
			h.a.connection.errors["note"] != "" ||
			h.a.connection.errors["form"] != ""
	})
	if h.a.connection.errors["note"] != "" || h.a.connection.errors["form"] != "" {
		t.Fatalf("clear note save errors: %v", h.a.connection.errors)
	}
	clearedStored, err := h.server.Vault.ServerNote(p.ID)
	if err != nil {
		t.Fatalf("read cleared note: %v", err)
	}
	if clearedStored.Manual != "" {
		t.Fatalf("cleared manual = %q, want empty", clearedStored.Manual)
	}
	if clearedStored.Discovered != originalNote.Discovered ||
		clearedStored.Details != originalNote.Details ||
		!clearedStored.CheckedAt.Equal(originalNote.CheckedAt) ||
		!reflect.DeepEqual(clearedStored.Target, originalNote.Target) {
		t.Fatalf("clear manual changed snapshot: %#v", clearedStored)
	}
	afterClearProfile := h.stored(p.ID)
	if afterClearProfile.Password != credentialsBefore.Password ||
		afterClearProfile.Network.Proxy.Password != credentialsBefore.Network.Proxy.Password {
		t.Fatal("clear note save changed retained credentials")
	}
	if h.fixture.Auths.Load() != authsBefore {
		t.Fatal("clear note save started SSH")
	}
}
