package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"whatsappsync/internal/storage"
)

type fakeBackend struct {
	toggled, purged string
	requested       string
	enabled         bool
	snapshot        Snapshot
}

func (f *fakeBackend) Connect(func(string)) error  { return nil }
func (f *fakeBackend) Refresh() error              { return nil }
func (f *fakeBackend) Snapshot() (Snapshot, error) { return f.snapshot, nil }
func (f *fakeBackend) Toggle(_ int64, g string, e bool) error {
	f.toggled = g
	f.enabled = e
	return nil
}
func (f *fakeBackend) Purge(_ int64, g string) error { f.purged = g; return nil }
func (f *fakeBackend) Messages(int64, string, storage.MessageQuery) (storage.MessagePage, error) {
	return storage.MessagePage{}, nil
}
func (f *fakeBackend) Logout() error                              { return nil }
func (f *fakeBackend) RequestHistory(_ int64, group string) error { f.requested = group; return nil }
func key(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "space":
		return tea.KeyPressMsg{Code: ' ', Text: " "}
	}
	return tea.KeyPressMsg{Code: []rune(s)[0], Text: s}
}
func update(m Model, msg tea.Msg) (Model, tea.Cmd) {
	model, cmd := m.Update(msg)
	return model.(Model), cmd
}
func fixtureModel() (Model, *fakeBackend) {
	f := &fakeBackend{snapshot: Snapshot{Account: 1, Groups: []storage.Group{{ID: "1@g.us", Name: "Alpha"}, {ID: "2@g.us", Name: "Beta"}}}}
	m := NewModel(f)
	m.snapshot = f.snapshot
	m.qrSink = func(string) {}
	return m, f
}
func TestTUIToggleFilterAndConfirmation(t *testing.T) {
	m, f := fixtureModel()
	m, _ = update(m, key("down"))
	m, cmd := update(m, key("space"))
	if cmd == nil {
		t.Fatal("toggle did not generate command")
	}
	cmd()
	if f.toggled != "2@g.us" || !f.enabled {
		t.Fatal("wrong group toggled")
	}
	m, _ = update(m, operationMsg{kind: "toggle"})
	m, _ = update(m, key("/"))
	m, _ = update(m, key("A"))
	m, _ = update(m, key("l"))
	if len(m.filtered()) != 1 {
		t.Fatal("group filter failed")
	}
	m, _ = update(m, key("enter"))
	m, _ = update(m, key("D"))
	if m.confirm != "purge" {
		t.Fatal("no purge confirmation")
	}
	m, _ = update(m, key("n"))
	if f.purged != "" {
		t.Fatal("purge happened without confirmation")
	}
	m, _ = update(m, key("D"))
	_, cmd = update(m, key("y"))
	if cmd == nil {
		t.Fatal("confirmed purge not scheduled")
	}
	cmd()
	if f.purged != "1@g.us" {
		t.Fatal("purged wrong group")
	}
}
func TestTUIRejectsStalePagesAndPreservesSelection(t *testing.T) {
	m, _ := fixtureModel()
	m.cursor = 1
	reordered := m.snapshot
	reordered.Groups = []storage.Group{m.snapshot.Groups[1], m.snapshot.Groups[0]}
	m, _ = update(m, snapshotMsg{snapshot: reordered})
	g, _ := m.selected()
	if g.ID != "2@g.us" {
		t.Fatal("selection jumped during refresh")
	}
	m.mode = "messages"
	m.groupID = "2@g.us"
	m.search = "new query"
	m.pageBusy = true
	m, _ = update(m, pageMsg{group: "2@g.us", search: "old query", page: storage.MessagePage{Items: []storage.Message{{Text: "stale"}}}})
	if len(m.page.Items) != 0 || !m.pageBusy {
		t.Fatal("stale asynchronous result replaced current query")
	}
}
func TestNarrowTUIAllowsFullTextAndSanitizesControls(t *testing.T) {
	m, _ := fixtureModel()
	m.mode = "messages"
	m.groupID = "1@g.us"
	m.groupName = "Alpha"
	m.page = storage.MessagePage{Items: []storage.Message{{Text: "real body\x1b]52;c;stolen\a\nsecond line", SenderID: "sender\x1b[2J"}}}
	m.width = 60
	m.height = 20
	m, _ = update(m, key("enter"))
	view := m.View().Content
	if !m.expanded || !strings.Contains(view, "real body") || strings.Contains(view, "stolen") || strings.Contains(view, "\x1b]52") {
		t.Fatal("full text inaccessible or terminal escape leaked")
	}
	for _, size := range [][2]int{{40, 12}, {60, 20}, {100, 30}, {160, 45}} {
		m.width = size[0]
		m.height = size[1]
		view = m.View().Content
		if len(strings.Split(view, "\n")) > size[1] {
			t.Fatal("view taller than terminal")
		}
		for _, line := range strings.Split(view, "\n") {
			if lipgloss.Width(line) > size[0] {
				t.Fatalf("view overflows width %d", size[0])
			}
		}
	}
}
func TestFilteringQDoesNotQuitAndPasteIsBounded(t *testing.T) {
	m, _ := fixtureModel()
	m, _ = update(m, key("/"))
	m, cmd := update(m, key("q"))
	if cmd != nil || m.filter != "q" {
		t.Fatal("q in filter quit application")
	}
	m, _ = update(m, tea.PasteMsg{Content: strings.Repeat("x", 10000)})
	if len([]rune(m.draft)) > 256 {
		t.Fatal("paste exceeded input limit")
	}
}

func TestMouseCheckboxSelectsAndTogglesIntendedGroup(t *testing.T) {
	m, f := fixtureModel()
	_, cmd := update(m, tea.MouseClickMsg{X: 5, Y: 6, Button: tea.MouseLeft})
	if cmd == nil {
		t.Fatal("checkbox click did not toggle capture")
	}
	cmd()
	if f.toggled != "2@g.us" || !f.enabled {
		t.Fatal("mouse toggled wrong group")
	}
}

func TestHistoryRetryTargetsOpenGroup(t *testing.T) {
	m, f := fixtureModel()
	m.mode = "messages"
	m.groupID = "2@g.us"
	_, cmd := update(m, key("h"))
	if cmd == nil {
		t.Fatal("no history action")
	}
	cmd()
	if f.requested != "2@g.us" {
		t.Fatal("history action targeted a different group")
	}
	if !strings.Contains(historyRequestLabel("waiting_anchor"), "known message") {
		t.Fatal("missing-anchor state is not explained")
	}
	if !strings.Contains(historyRequestLabel("no_progress"), "unknown") {
		t.Fatal("empty response claims complete history")
	}
}
