package cred_default

import (
	"bytes"
	"crypto"
	"crypto/rand"
	"crypto/x509"
	"encoding/binary"

	"math/big"
	"sync"
	"time"

	cred "github.com/abchain/fabric/core/cred"
	pb "github.com/abchain/fabric/protos"
	"github.com/spf13/viper"
)

func driverImpl(vp *viper.Viper, drv *Credentials_PeerDriver) error {

}
