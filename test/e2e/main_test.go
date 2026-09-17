package e2e

import (
	"os"
	"testing"

	"github.com/spignelon/ipgrab/internal/mirror"
)

// TestMain flips mirror.AllowPrivateTargetsForTesting on for this whole
// suite, since the clone-engine tests stand up local httptest "target"
// servers (127.0.0.1) instead of depending on the real internet — which
// guardURL would otherwise correctly refuse to fetch. Tests that need to
// verify the guard itself (see TestMirrorSSRFGuardBlocksPrivateTargets)
// temporarily flip it back off around their own scope.
func TestMain(m *testing.M) {
	mirror.AllowPrivateTargetsForTesting = true
	os.Exit(m.Run())
}
