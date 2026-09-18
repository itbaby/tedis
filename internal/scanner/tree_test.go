package scanner

import (
	"reflect"
	"testing"
)

func labels(rows []Row) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		mark := " "
		if r.Folder {
			if r.Expanded {
				mark = "▾"
			} else {
				mark = "▸"
			}
		}
		out[i] = mark + r.Label
	}
	return out
}

func TestTreeFold1DefaultCollapsed(t *testing.T) {
	tr := NewTree(":", 1)
	for _, k := range []string{"users:1001", "users:1002:name", "session:a", "plain"} {
		tr.Add(k)
	}
	want := []string{" *", " plain", "▸session", "▸users"}
	if got := labels(tr.Rows()); !reflect.DeepEqual(got, want) {
		t.Fatalf("rows=%v want=%v", got, want)
	}
}

func TestTreeCountsAndDuplicates(t *testing.T) {
	tr := NewTree(":", 1)
	tr.Add("users:1")
	tr.Add("users:2")
	tr.Add("users:2") // SCAN duplicate must not double-count
	tr.Add("session:x")
	if tr.Total() != 3 {
		t.Fatalf("total=%d want 3", tr.Total())
	}
	rows := tr.Rows()
	byLabel := map[string]Row{}
	for _, r := range rows {
		byLabel[r.Label] = r
	}
	if byLabel["users"].Count != 2 || byLabel["session"].Count != 1 || byLabel["plain"].Count != 0 {
		t.Fatalf("counts: %+v", rows)
	}
}

func TestTreeToggleAndExpand(t *testing.T) {
	tr := NewTree(":", 1)
	tr.Add("users:1001")
	tr.Add("users:1002:name")
	tr.Add("session:x")

	if !tr.Toggle("users") {
		t.Fatal("toggle users should change state")
	}
	rows := tr.Rows()
	// after expand: users children are leaf rows; fold=1 merges deeper parts
	want := []string{" *", "▸session", "▾users", " 1001", " 1002:name"}
	if got := labels(rows); !reflect.DeepEqual(got, want) {
		t.Fatalf("rows=%v want=%v", got, want)
	}
	if tr.Toggle("users:1001") { // leaf: nothing to toggle
		t.Fatal("toggle on leaf should be a no-op")
	}
	if !tr.Toggle("users") {
		t.Fatal("toggle back should change state")
	}
	if got := labels(tr.Rows()); len(got) != 3 {
		t.Fatalf("collapsed rows=%v", got)
	}
}

func TestTreeFoldLevel2(t *testing.T) {
	tr := NewTree(":", 2)
	tr.Add("users:1001:name")
	tr.Add("users:1001:email")
	tr.Add("users:1002:name")
	tr.Add("cfg:db:host")

	if got := labels(tr.Rows()); !reflect.DeepEqual(got, []string{" *", "▸cfg", "▸users"}) {
		t.Fatalf("top: %v", got)
	}
	tr.Toggle("users")
	if got := labels(tr.Rows()); !reflect.DeepEqual(got,
		[]string{" *", "▸cfg", "▾users", "▸1001", "▸1002"}) {
		t.Fatalf("level1: %v", got)
	}
	tr.Toggle("users:1001")
	// fold=2: parts beyond depth 2 merge into the leaf label ("name")
	if got := labels(tr.Rows()); !reflect.DeepEqual(got,
		[]string{" *", "▸cfg", "▾users", "▾1001", " email", " name", "▸1002"}) {
		t.Fatalf("level2: %v", got)
	}
}

func TestTreeFolderAndLeafSameNode(t *testing.T) {
	tr := NewTree(":", 1)
	tr.Add("users")      // key "users"
	tr.Add("users:1001") // and keys under it
	rows := tr.Rows()
	// "users" renders as folder (documented: folder wins over key row)
	if len(rows) != 2 || !rows[1].Folder || rows[1].Count != 2 {
		t.Fatalf("rows=%+v", rows)
	}
}

func TestTreeSeparatorCustom(t *testing.T) {
	tr := NewTree(".", 1)
	tr.Add("a.b.c")
	rows := tr.Rows()
	if rows[1].Label != "a" || !rows[1].Folder {
		t.Fatalf("rows=%+v", rows)
	}
}
