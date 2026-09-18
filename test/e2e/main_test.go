package e2e

import (
	"os"
	"testing"

	"github.com/spignelon/netra/internal/netguard"
)

// TestMain flips netguard.AllowPrivateForTesting on for this whole suite,
// since the clone-engine and webhook tests stand up local httptest "target"
// servers (127.0.0.1) instead of depending on the real internet — which the
// guard would otherwise correctly refuse to connect to. Tests that need to
// verify the guard itself (see TestMirrorSSRFGuardBlocksPrivateTargets)
// temporarily flip it back off around their own scope.
func TestMain(m *testing.M) {
	netguard.AllowPrivateForTesting = true
	os.Exit(m.Run())
}
