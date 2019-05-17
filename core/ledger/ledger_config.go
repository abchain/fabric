package ledger

import (
	"github.com/spf13/viper"
)

type ledgerConfig struct {
	*viper.Viper
}

func NewLedgerConfig(vp *viper.Viper) *ledgerConfig {

	return &ledgerConfig{vp}
}

//some legacy if API ledgerConfig is not avaliable
func sanityCheckLedger() bool {
	return viper.GetBool("ledger.sanitycheck")
}
