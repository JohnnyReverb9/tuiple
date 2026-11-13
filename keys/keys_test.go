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

// Global keys are matched before the file list gets a look, so a key
// bound in both scopes would make the list's binding unreachable — and
// the per-scope check above would not notice.
func TestNoKeyIsShadowedAcrossScopes(t *testing.T) {
	global := make(map[string]Action)
	for _, b := range Defaults {
		if b.Scope == ScopeGlobal {
			for _, k := range b.Keys {
				global[k] = b.Action
			}
		}
	}
	for _, b := range Defaults {
		if b.Scope != ScopeList {
			continue
		}
		for _, k := range b.Keys {
			if owner, taken := global[k]; taken {
				t.Errorf("key %q of %q is shadowed by the global action %q", k, b.Action, owner)
			}
		}
	}
}

func TestPretty(t *testing.T) {
	for key, want := range map[string]string{
		"t":             "t",
		" ":             "Space",
		"ctrl+p":        "Ctrl+P",
		"shift+tab":     "Shift+Tab",
		"cmd+e":         "⌘E",
		"cmd+shift+a":   "⇧⌘A",
		"cmd+alt+left":  "⌥⌘←",
		"cmd+backspace": "⌘⌫",
		"cmd+[":         "⌘[",
		"+":             "+",
	} {
		if got := Pretty(key); got != want {
			t.Errorf("Pretty(%q) = %q, want %q", key, got, want)
		}
	}
}

// Rebinding an action must also retire the hand-written help label that
// described its default keys.
func TestOverrideDropsTheStaleHelpLabel(t *testing.T) {
	t.Cleanup(func() { SetOverrides(nil) })

	SetOverrides(map[string][]string{string(HalfPageUp): {"ctrl+b"}})
	b, ok := Find(HalfPageUp)
	if !ok {
		t.Fatal("binding not found")
	}
	if got := b.Display(); got != "Ctrl+B" {
		t.Errorf("help shows %q for the rebound action, want Ctrl+B", got)
	}
}
