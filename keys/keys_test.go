package keys

import "testing"

// A key may mean only one thing per scope. A duplicate would silently
// shadow whichever binding the table happens to list second — the class
// of bug this package exists to make impossible.
func TestNoDuplicateKeysWithinScope(t *testing.T) {
	seen := make(map[lookupKey]Action)
	for _, b := range Defaults {
		for _, k := range b.Keys {
			lk := lookupKey{b.Scope, k}
			if prev, dup := seen[lk]; dup {
				t.Errorf("key %q is bound to both %q and %q in the same scope", k, prev, b.Action)
			}
			seen[lk] = b.Action
		}
	}
}

func TestEveryBindingIsUsable(t *testing.T) {
	actions := make(map[Action]bool)
	for _, b := range Defaults {
		if len(b.Keys) == 0 {
			t.Errorf("%q ships without a key", b.Action)
		}
		if b.Desc == "" {
			t.Errorf("%q has no description — it would render blank in help", b.Action)
		}
		if b.Category == "" {
			t.Errorf("%q has no category — it would not appear in help at all", b.Action)
		}
		if actions[b.Action] {
			t.Errorf("%q is listed twice", b.Action)
		}
		actions[b.Action] = true
	}
}

func TestOverridesReplaceAndUnbind(t *testing.T) {
	t.Cleanup(func() { SetOverrides(nil) })

	SetOverrides(map[string][]string{
		string(Delete):   {"x"},
		string(Quit):     {},
		"no-such-action": {"y"},
	})

	if !Is(ScopeList, "x", Delete) {
		t.Error("x should delete after the override")
	}
	if _, bound := Match(ScopeList, "d"); bound {
		t.Error("d should no longer be bound after being overridden")
	}
	if _, bound := Match(ScopeGlobal, "q"); bound {
		t.Error("an empty key list should unbind the action")
	}

	// Defaults must survive an override of unrelated actions, and the
	// unknown name must not have corrupted the table.
	if !Is(ScopeGlobal, "?", Help) {
		t.Error("untouched bindings should keep working")
	}

	SetOverrides(nil)
	if !Is(ScopeList, "d", Delete) {
		t.Error("clearing overrides should restore the defaults")
	}
}

func TestHelpRendering(t *testing.T) {
	if len(Categories()) == 0 {
		t.Fatal("help would render empty")
	}
	for _, cat := range Categories() {
		if len(InCategory(cat)) == 0 {
			t.Errorf("category %q lists no bindings", cat)
		}
	}
	// Display collapses a multi-key binding into one readable cell.
	for _, b := range Defaults {
		if b.Display() == "" {
			t.Errorf("%q renders as an empty key cell", b.Action)
		}
	}
}
