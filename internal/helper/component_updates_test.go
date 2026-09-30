package helper

import (
	"testing"
	"time"

	"linux-dashboard/OxidiLily/internal/helperproto"
)

func TestComponentUpdatesOnlyForInstalledComponents(t *testing.T) {
	lupakanCacheUpdates()
	base := []helperproto.ComponentStatus{{Name: "9router", Installed: false}, {Name: "hermes", Installed: false}}
	if got := ComponentUpdates(base, nil); len(got) != 0 {
		t.Fatalf("unexpected updates for absent components: %+v", got)
	}
}

func TestComponentUpdatesKeepsInstalledWithoutKnownVersion(t *testing.T) {
	lupakanCacheUpdates()
	base := []helperproto.ComponentStatus{{Name: "9router", Installed: true}, {Name: "nginx", Installed: true}}
	// Pertama kali bisa return kosong karena compute di background.
	// Poll sampai background selesai (cache terisi).
	var got []helperproto.ComponentStatus
	deadline := time.After(30 * time.Second)
	for {
		got = ComponentUpdates(base, nil)
		if len(got) > 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for background compute; got empty")
		case <-time.After(200 * time.Millisecond):
		}
	}
	if len(got) != 1 || got[0].Name != "9router" || got[0].VersiBaru != "" {
		t.Fatalf("unexpected update candidates: %+v", got)
	}
}
