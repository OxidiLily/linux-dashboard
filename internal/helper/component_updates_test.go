package helper

import (
	"testing"

	"linux-dashboard/OxidiLily/internal/helperproto"
)

func TestComponentUpdatesOnlyForInstalledComponents(t *testing.T) {
	base := []helperproto.ComponentStatus{{Name: "9router", Installed: false}, {Name: "hermes", Installed: false}}
	if got := ComponentUpdates(base, nil); len(got) != 0 {
		t.Fatalf("unexpected updates for absent components: %+v", got)
	}
}

func TestComponentUpdatesKeepsInstalledWithoutKnownVersion(t *testing.T) {
	base := []helperproto.ComponentStatus{{Name: "9router", Installed: true}, {Name: "nginx", Installed: true}}
	got := ComponentUpdates(base, nil)
	if len(got) != 1 || got[0].Name != "9router" || got[0].VersiBaru != "" {
		t.Fatalf("unexpected update candidates: %+v", got)
	}
}
