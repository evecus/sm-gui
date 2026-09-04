package node

import (
	"path/filepath"
	"testing"
)

func newTestNode(id, name, protocol, addr string, port int) Node {
	return Node{
		ID: id, Name: name, Protocol: protocol, Address: addr, Port: port,
		SS: &SSConfig{Method: "aes-256-gcm", Password: "pw-" + id},
	}
}

func TestStoreGroups(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "nodes.db")

	s := NewStore(dbPath)
	if err := s.Load(); err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	// default group exists on startup
	groups := s.GetGroups()
	if len(groups) != 1 || groups[0].ID != "default" || groups[0].Name != "默认" || !groups[0].IsDefault {
		t.Fatalf("default group wrong: %+v", groups)
	}

	// create a group right after 默认
	g, err := s.AddGroup("机场A", "default")
	if err != nil {
		t.Fatalf("AddGroup: %v", err)
	}
	if g.ID == "" || g.IsDefault {
		t.Fatalf("AddGroup returned wrong group: %+v", g)
	}
	// duplicate name rejected
	if _, err := s.AddGroup("机场A", ""); err == nil {
		t.Error("duplicate group name should fail")
	}

	// nodes go into the specified group
	s.AddMany([]Node{
		newTestNode("n1", "A组节点1", "ss", "1.1.1.1", 443),
		newTestNode("n2", "A组节点2", "ss", "1.1.1.2", 443),
		newTestNode("d1", "默认节点", "ss", "2.2.2.2", 443),
	})
	s.Update(Node{ID: "n1", Name: "A组节点1", Protocol: "ss", Address: "1.1.1.1", Port: 443, GroupID: g.ID,
		SS: &SSConfig{Method: "aes-256-gcm", Password: "x"}})
	s.Update(Node{ID: "n2", Name: "A组节点2", Protocol: "ss", Address: "1.1.1.2", Port: 443, GroupID: g.ID,
		SS: &SSConfig{Method: "aes-256-gcm", Password: "x"}})

	all := s.GetAll()
	var inA, inDefault int
	for _, n := range all {
		switch n.GroupID {
		case g.ID:
			inA++
		case "default", "":
			inDefault++
		}
	}
	if inA != 2 || inDefault != 1 {
		t.Errorf("group membership wrong: inA=%d inDefault=%d", inA, inDefault)
	}

	// Move is scoped to the group: moving d1 (default group) must not affect A组
	// move d1 up — it's the only node in default group → fails
	if s.Move("d1", -1) {
		t.Error("move single-node group should fail")
	}
	// move n2 up within A组: order n1,n2 → n2,n1
	if !s.Move("n2", -1) {
		t.Fatal("Move(n2,-1) should succeed")
	}
	// verify default group node untouched & A组 order changed
	var aOrder []string
	for _, n := range s.GetAll() {
		if n.GroupID == g.ID {
			aOrder = append(aOrder, n.ID)
		}
	}
	if len(aOrder) != 2 || aOrder[0] != "n2" || aOrder[1] != "n1" {
		t.Errorf("group move wrong: %v", aOrder)
	}

	// rename
	if err := s.RenameGroup("default", "X"); err == nil {
		t.Error("renaming default group should fail")
	}
	if err := s.RenameGroup(g.ID, "机场B"); err != nil {
		t.Errorf("rename failed: %v", err)
	}

	// delete group → its nodes move to default
	if err := s.DeleteGroup("default"); err == nil {
		t.Error("deleting default group should fail")
	}
	if err := s.DeleteGroup(g.ID); err != nil {
		t.Fatalf("delete group: %v", err)
	}
	groups = s.GetGroups()
	if len(groups) != 1 {
		t.Fatalf("groups after delete = %d, want 1", len(groups))
	}
	inDefault = 0
	for _, n := range s.GetAll() {
		if n.GroupID == "default" || n.GroupID == "" {
			inDefault++
		}
	}
	if inDefault != 3 {
		t.Errorf("nodes should move to default after group delete, got %d", inDefault)
	}

	// AddGroup with empty afterID appends; ordering: 默认 always first
	g2, err := s.AddGroup("新分组", "")
	if err != nil {
		t.Fatalf("AddGroup2: %v", err)
	}
	groups = s.GetGroups()
	if len(groups) != 2 || groups[0].ID != "default" || groups[1].ID != g2.ID {
		t.Errorf("group order wrong: %+v", groups)
	}

	// persistence across reopen
	s.Close()
	s2 := NewStore(dbPath)
	defer s2.Close()
	if gs := s2.GetGroups(); len(gs) != 2 || gs[0].Name != "默认" || gs[1].Name != "新分组" {
		t.Errorf("groups after reopen wrong: %+v", gs)
	}
	if s2.Count() != 3 {
		t.Errorf("nodes after reopen = %d, want 3", s2.Count())
	}
}

func TestStoreCRUDAndOrder(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "nodes.db")

	s := NewStore(dbPath)
	if err := s.Load(); err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	// AddMany
	s.AddMany([]Node{
		newTestNode("1", "节点一", "ss", "1.1.1.1", 443),
		newTestNode("2", "节点二", "vless", "2.2.2.2", 8443),
		newTestNode("3", "节点三", "trojan", "3.3.3.3", 443),
	})

	if got := len(s.GetAll()); got != 3 {
		t.Fatalf("want 3 nodes, got %d", got)
	}
	// order check
	all := s.GetAll()
	if all[0].ID != "1" || all[1].ID != "2" || all[2].ID != "3" {
		t.Errorf("unexpected order: %v %v %v", all[0].ID, all[1].ID, all[2].ID)
	}

	// Get
	n := s.Get("2")
	if n == nil || n.Protocol != "vless" {
		t.Fatalf("Get(2) = %+v", n)
	}

	// Move down: 2 → position 3
	if !s.Move("2", 1) {
		t.Fatal("Move(2, +1) should succeed")
	}
	all = s.GetAll()
	if all[2].ID != "2" {
		t.Errorf("after move down order wrong: %v %v %v", all[0].ID, all[1].ID, all[2].ID)
	}
	// Move again → already at bottom, should fail
	if s.Move("2", 1) {
		t.Error("Move at bottom should fail")
	}
	// Move up: 2 back
	if !s.Move("2", -1) {
		t.Fatal("Move(2, -1) should succeed")
	}
	if s.Move("1", -1) {
		t.Error("Move at top should fail")
	}

	// Update
	updated := newTestNode("1", "改名了", "ss", "9.9.9.9", 1234)
	s.Update(updated)
	n = s.Get("1")
	if n == nil || n.Name != "改名了" || n.Address != "9.9.9.9" || n.Port != 1234 {
		t.Errorf("Update failed: %+v", n)
	}
	if n.SS == nil || n.SS.Method != "aes-256-gcm" {
		t.Errorf("nested config lost after update: %+v", n.SS)
	}

	// Delete
	s.Delete("3")
	if s.Get("3") != nil {
		t.Error("Delete failed")
	}
	if s.Count() != 2 {
		t.Errorf("count = %d, want 2", s.Count())
	}

	// RemoveBySubscription
	s.AddMany([]Node{
		{ID: "s1", Name: "sub节点", Protocol: "ss", Address: "4.4.4.4", Port: 80, SubURL: "https://sub/x"},
		{ID: "s2", Name: "手动节点", Protocol: "ss", Address: "5.5.5.5", Port: 80},
	})
	s.RemoveBySubscription("https://sub/x")
	if s.Get("s1") != nil {
		t.Error("RemoveBySubscription failed")
	}
	if s.Get("s2") == nil {
		t.Error("RemoveBySubscription removed wrong node")
	}

	// ─── persistence across reopen ───
	s.Close()
	s2 := NewStore(dbPath)
	defer s2.Close()
	if err := s2.Load(); err != nil {
		t.Fatalf("reopen: %v", err)
	}
	got := s2.Get("1")
	if got == nil || got.Name != "改名了" {
		t.Errorf("persistence failed: %+v", got)
	}
	if s2.Count() != 3 { // 1, 2, s2
		t.Errorf("count after reopen = %d, want 3", s2.Count())
	}
	// order persisted: "1" still before "2", "s2" last
	ids := []string{}
	for _, nn := range s2.GetAll() {
		ids = append(ids, nn.ID)
	}
	if ids[0] != "1" || ids[1] != "2" || ids[2] != "s2" {
		t.Errorf("order after reopen wrong: %v", ids)
	}
}

