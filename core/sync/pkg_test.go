package sync

import (
	"github.com/abchain/fabric/core/ledger/testutil"
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	testutil.SetupTestConfig()
	os.Exit(m.Run())
}
