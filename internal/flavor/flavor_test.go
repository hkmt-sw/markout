package flavor

import "testing"

func TestRegistry(t *testing.T) {
	seen := map[string]bool{}
	for _, f := range All() {
		if f.ID == "" || f.Name == "" || f.Description == "" {
			t.Errorf("flavor %+v is missing an ID, name or description", f)
		}
		if seen[f.ID] {
			t.Errorf("duplicate flavor ID %q", f.ID)
		}
		seen[f.ID] = true
		if got, ok := ByID(f.ID); !ok || got.ID != f.ID {
			t.Errorf("ByID(%q) did not find the flavor", f.ID)
		}
	}
	if Default().ID != DefaultID {
		t.Errorf("Default() = %q, want %q", Default().ID, DefaultID)
	}
	for alias, id := range aliases {
		if !seen[id] {
			t.Errorf("alias %q points at unknown flavor %q", alias, id)
		}
	}
}

func TestByID(t *testing.T) {
	for in, want := range map[string]string{"GFM": "github", " glfm ": "gitlab", "YouTrack": "youtrack", "ado": "azure"} {
		if got, ok := ByID(in); !ok || got.ID != want {
			t.Errorf("ByID(%q) = %q, %v; want %q", in, got.ID, ok, want)
		}
	}
	if _, ok := ByID("nope"); ok {
		t.Error("ByID accepted an unknown flavor")
	}
}

// All must hand out a copy so callers cannot alter the registry.
func TestAllIsACopy(t *testing.T) {
	a := All()
	a[0].ID = "changed"
	if All()[0].ID != DefaultID {
		t.Error("All() exposes the registry's backing array")
	}
}
