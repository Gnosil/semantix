package serve

import (
	"testing"

	"semantix/harness/internal/testenv"
)

func TestMain(m *testing.M) {
	testenv.RunWithIsolatedUserState(m)
}
