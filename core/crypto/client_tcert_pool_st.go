/*
Copyright IBM Corp. 2016 All Rights Reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

		 http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package crypto

import (
	"encoding/hex"
	"fmt"
	"sort"
	"sync"
	"time"
	"errors"
	"github.com/op/go-logging"
	"github.com/abchain/fabric/core/crypto/primitives"
)

var poolLogger = logging.MustGetLogger("tcertpool")

//TCertDBBlock is an object used to store the TCert in the database. A raw field is used to represent the TCert and the preK0, a string field is use to the attributesHash.
type TCertDBBlock struct {
	tCertDER       []byte
	attributesHash string
	preK0          []byte
	// counter        int
}

type tCertPoolSingleThreadImpl struct {
	client *clientImpl

	// empty bool
	// length map[string]int
	// tCerts map[string][]*TCertBlock

	tcblMap      map[string]tcertBlockList

	m sync.Mutex
}

//Start starts the pool processing.
func (tCertPool *tCertPoolSingleThreadImpl) Start() (err error) {
	tCertPool.m.Lock()
	defer tCertPool.m.Unlock()

	tCertPool.client.Debug("Starting TCert Pool...")

	// Load unused TCerts if any
	tCertDBBlocks, err := tCertPool.client.ks.loadUnusedTCerts()
	if err != nil {
		tCertPool.client.Errorf("Failed loading TCerts from cache: [%s]", err)

		return
	}

	if len(tCertDBBlocks) > 0 {

		tCertPool.client.Debug("TCerts in cache found! Loading them...")

		for _, tCertDBBlock := range tCertDBBlocks {
			tCertBlock, err := tCertPool.client.getTCertFromDER(tCertDBBlock)
			if err != nil {
				tCertPool.client.Errorf("Failed paring TCert [% x]: [%s]", tCertDBBlock.tCertDER, err)

				continue
			}
			tCertPool.AddTCert(tCertBlock)
		}
	} //END-IF

	return
}

//Stop stops the pool.
func (tCertPool *tCertPoolSingleThreadImpl) Stop() (err error) {
	tCertPool.m.Lock()
	defer tCertPool.m.Unlock()
	for k := range tCertPool.tcblMap {
		certList := tCertPool.tcblMap[k]
		tCertPool.client.ks.storeUnusedTCerts(certList.GetUnusedTCertBlocks())
	}

	tCertPool.client.Debug("Store unused TCerts...done!")

	return
}

//calculateAttributesHash generates a unique hash using the passed attributes.
func calculateAttributesHash(attributes []string) (attrHash string) {

	keys := make([]string, len(attributes))

	for _, k := range attributes {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	values := make([]byte, len(keys))

	for _, k := range keys {
		vb := []byte(k)
		for _, bval := range vb {
			values = append(values, bval)
		}
	}
	attributesHash := primitives.Hash(values)
	return hex.EncodeToString(attributesHash)

}

//GetNextTCert returns a TCert from the pool valid to the passed attributes. If no TCert is available TCA is invoked to generate it.
func (tCertPool *tCertPoolSingleThreadImpl) GetNextTCerts(nCerts int, attributes ...string) ([]*TCertBlock, error) {
	attributesHash := calculateAttributesHash(attributes)
	blocks := make([]*TCertBlock, nCerts)
	for i := 0; i < nCerts; i++ {
		block, err := tCertPool.getNextTCert(attributesHash, attributes...)
		if err != nil {
			return nil, err
		}
		blocks[i] = block
	}
	return blocks, nil
}

// func (tCertPool *tCertPoolSingleThreadImpl) getNextTCert(attributes ...string) (tCert *TCertBlock, err error) {

// 	tCertPool.m.Lock()
// 	defer tCertPool.m.Unlock()

// 	attributesHash := calculateAttributesHash(attributes)

// 	poolLen := tCertPool.length[attributesHash]

// 	if poolLen <= 0 {
// 		// Reload
// 		if err := tCertPool.client.getTCertsFromTCA(attributesHash, attributes, tCertPool.client.conf.getTCertBatchSize()); err != nil {
// 			return nil, fmt.Errorf("Failed loading TCerts from TCA")
// 		}
// 	}

// 	tCert = tCertPool.tCerts[attributesHash][tCertPool.length[attributesHash]-1]

// 	tCertPool.length[attributesHash] = tCertPool.length[attributesHash] - 1

// 	return tCert, nil
// }



func (tCertPool *tCertPoolSingleThreadImpl) init(client *clientImpl) error {
	tCertPool.client = client
	tCertPool.client.Debug("Init TCert Pool...")
	tCertPool.tcblMap = make(map[string]tcertBlockList)
	return nil
}

func (tCertPool *tCertPoolSingleThreadImpl) getTCertBlockList(attrHash string) tcertBlockList {
	v, ok := tCertPool.tcblMap[attrHash]
	if !ok {
		isReuseable := tCertPool.client.conf.getTCertReusedEnable()
		batchSize := tCertPool.client.conf.getTCertBatchSize()
		if isReuseable {
			rr := tCertPool.client.conf.getTCertReusedRoundRobin()
			counterLimit := tCertPool.client.conf.getTCertReusedBatch()
			reusedUpdateSecond := tCertPool.client.conf.getTCertReusedUpdateSecond()
			if rr > 1 {
				 tcbl := newTCertBlockList(attrHash, rr, "RoundRobin").(*tcertBlockListRoundRobin)
				 tcbl.counterLimit = counterLimit
				 tcbl.reusedUpdateSecond = reusedUpdateSecond
				 tCertPool.tcblMap[attrHash] = tcbl
				 return tcbl
			} else {
				tcbl := newTCertBlockList(attrHash, batchSize, "Reuse").(*tcertBlockListReuse)
				tcbl.counterLimit = counterLimit
				tcbl.reusedUpdateSecond = reusedUpdateSecond
				tCertPool.tcblMap[attrHash] = tcbl
				return tcbl
			}
		}
		tcbl := newTCertBlockList(attrHash, batchSize, "Normal")
		tCertPool.tcblMap[attrHash] = tcbl
		return tcbl
	}
	return v
}


func (tCertPool *tCertPoolSingleThreadImpl) getNextTCert(attrHash string, attributes ...string) (tCert *TCertBlock, err error) {
	tCertPool.m.Lock()
	defer tCertPool.m.Unlock()

	tcbl := tCertPool.getTCertBlockList(attrHash)

	tCertBlk, err := tcbl.Get()
	if err != nil {
		num := tcbl.GetUpdateNum()
		tCertPool.supplyTCerts(num, attrHash, attributes...)
	}
	tCertBlk, err = tcbl.Get()
	if err != nil {
		return nil, err
	}

	return tCertBlk, nil
}

func (tCertPool *tCertPoolSingleThreadImpl) supplyTCerts(num int, attributesHash string, attributes ...string) error {
	tCertPool.m.Lock()
	defer tCertPool.m.Unlock()

	if err := tCertPool.client.getTCertsFromTCA(attributesHash, attributes, num); err != nil {
		return fmt.Errorf("Failed loading TCerts from TCA")
	}

	// blks, err := tCertPool.tcaClient.getTCertsFromTCA(attributesHash, attributes, num); 
	// if err != nil {
	// 	return fmt.Errorf("Failed loading TCerts from TCA")
	// }
	// for _, blk := range blks {
	// 	tCertPool.AddTCert(blk)
	// } 

	return nil
}

// AddTCert adds a TCert into the pool is invoked by the client after TCA is called.
func (tCertPool *tCertPoolSingleThreadImpl) AddTCert(tCertBlock *TCertBlock) error {
	tCertPool.client.Debugf("Adding new Cert to tCertPool")

	tcbl := tCertPool.getTCertBlockList(tCertBlock.attributesHash)
	tcbl.Add(tCertBlock)
	return nil
}

const (
	fivemin = time.Minute * 5
	oneweek = time.Hour * 24 * 7
)

//TCertBlock is an object that include the generated TCert and the attributes used to generate it.
type TCertBlock struct {
	tCert          tCert
	attributesHash string
	counter        int
}

func (tCertBlock *TCertBlock) GetTCert() tCert{
	return tCertBlock.tCert
}

func (tCertBlock *TCertBlock) GetAttrHash() string{
	return tCertBlock.attributesHash
}

// expired if tcert NotAfter in 5 min
func (tcertBlock *TCertBlock) isExpired() bool {
	tsNow := time.Now()
	notAfter := tcertBlock.GetTCert().GetCertificate().NotAfter
	if tsNow.Add(fivemin).After(notAfter) {
		return true
	}
	return false
}

func (tcertBlock *TCertBlock) isCounterOverflow(counterLimit int) bool {
	return counterLimit > 0 && tcertBlock.counter >= counterLimit 
}	
// expired if tcert NotAfter in 5 min
func (tcertBlock *TCertBlock) isUpdateExpired(reusedUpdateSecond int) bool {
	notAfter := tcertBlock. GetTCert().GetCertificate().NotAfter
	tsNow := time.Now()
	if reusedUpdateSecond == 0 {
		// 1 week 
		if tsNow.Add(oneweek).After(notAfter) {
			return true
		} else {  // 2/3 expired
			notBefore := tcertBlock.GetTCert().GetCertificate().NotBefore
			timeDel := notAfter.Sub(notBefore) 
			if tsNow.Add(timeDel * 1 / 3).After(notAfter) {
				return true
			}
		}
	} else {
		if tsNow.Add(time.Duration(reusedUpdateSecond)).After(notAfter) {
			return true
		}
	}
	return false
}

type tcertBlockList interface {
	Add(tcBlk *TCertBlock)
	Get() (*TCertBlock, error)
	GetUpdateNum() int
	GetUnusedTCertBlocks() []*TCertBlock
}

func newTCertBlockList(attrHash string, size int, mode string) tcertBlockList {
	normal := &tcertBlockListNormal{
		attrHash: attrHash,
		blkList: make([]*TCertBlock, size),
		len: 0,
		size: size,
	}
	switch mode {
	case "Normal":
		return normal
	case "Reuse":
		return &tcertBlockListReuse{
			tcertBlockListNormal: normal,
		}
	case "RoundRobin":
		ru := &tcertBlockListReuse{
			tcertBlockListNormal: normal,
		}
		return &tcertBlockListRoundRobin{
			tcertBlockListReuse: ru,
		}
	}
	return normal
} 

type tcertBlockListNormal struct {
	attrHash    string
	blkList     []*TCertBlock
	len         int
	size        int

	m           sync.Mutex
}

func (tcbl *tcertBlockListNormal) Add(tcBlk *TCertBlock) {
	tcbl.m.Lock()
	defer tcbl.m.Unlock()

	tcbl.len = tcbl.len + 1
	tcbl.blkList[tcbl.len] = tcBlk
	poolLogger.Debugf("#TCBL ADD# hash: %s len: %s size: %s  ", tcbl.attrHash, tcbl.len, tcbl.size)
}

func (tcbl *tcertBlockListNormal) Get() (*TCertBlock, error) {
	tcbl.m.Lock()
	defer tcbl.m.Unlock()

	tcbl.removeExpired()

	if tcbl.len < 0 {
		return nil, errors.New("empty")
	}

	tcBlk := tcbl.blkList[tcbl.len]
	tcbl.len = tcbl.len - 1
	poolLogger.Debugf("#TCBL Get# hash: %s len: %s size: %s  ", tcbl.attrHash, tcbl.len, tcbl.size)
	return tcBlk, nil
}

func (tcbl *tcertBlockListNormal) GetUnusedTCertBlocks() []*TCertBlock {
	return tcbl.blkList[:tcbl.len]
}

func (tcbl *tcertBlockListNormal) removeExpired() {
	tcbl.m.Lock()
	defer tcbl.m.Unlock()

	for tcbl.len >= 0 {
		block := tcbl.blkList[tcbl.len]
		if block.isExpired() {
			tcbl.len = tcbl.len - 1
		} else {
			break
		}
	}
}

func (tcbl *tcertBlockListNormal) GetUpdateNum() int {
	return tcbl.size
}

type tcertBlockListReuse struct {
	*tcertBlockListNormal

	counterLimit       int
	reusedUpdateSecond int

	m           sync.Mutex
}

func (tcbl *tcertBlockListReuse) Add(tcBlk *TCertBlock) {
	tcbl.m.Lock()
	defer tcbl.m.Unlock()

	tcbl.len = tcbl.len + 1
	tcbl.blkList[tcbl.len] = tcBlk
}

func (tcbl *tcertBlockListReuse) Get() (*TCertBlock, error) {
	tcbl.m.Lock()
	defer tcbl.m.Unlock()

	tcbl.removeExpired()
	tcbl.removeUpdateExpired()
	tcbl.removeCounterOverflow()

	if (tcbl.len < 0) {
		return nil, errors.New("empty")
	}

	tcBlk := tcbl.blkList[tcbl.len]
	tcBlk.counter = tcBlk.counter + 1

	return tcBlk, nil
}


func (tcbl *tcertBlockListReuse) removeUpdateExpired() {
	tcbl.m.Lock()
	defer tcbl.m.Unlock()

	for tcbl.len >= 0 {
		block := tcbl.blkList[tcbl.len]
		if block.isUpdateExpired(tcbl.reusedUpdateSecond) {
			tcbl.len = tcbl.len - 1
		} else {
			break
		}
	}
}

func (tcbl *tcertBlockListReuse) removeCounterOverflow() {
	tcbl.m.Lock()
	defer tcbl.m.Unlock()
	
	for tcbl.len >= 0 {
		block := tcbl.blkList[tcbl.len]
		if block.isCounterOverflow(tcbl.counterLimit) {
			tcbl.len = tcbl.len - 1
		} else {
			break
		}
	}
}


type tcertBlockListRoundRobin struct {
	*tcertBlockListReuse

	cursor      int

	m           sync.Mutex
}

func (tcbl *tcertBlockListRoundRobin) Add(tcBlk *TCertBlock) {
	tcbl.m.Lock()
	defer tcbl.m.Unlock()
	if tcbl.len >= tcbl.size {
		return
	}
	tcbl.blkList[tcbl.len] = tcBlk
	tcbl.len = tcbl.len + 1
}

func (tcbl *tcertBlockListRoundRobin) Get() (*TCertBlock, error) {
	tcbl.m.Lock()
	defer tcbl.m.Unlock()

	tcbl.removeExpired()
	tcbl.removeUpdateExpired()
	tcbl.removeCounterOverflow()

	if (tcbl.cursor >= tcbl.len) {
		return nil, errors.New("overLength")
	}

	tcBlk := tcbl.blkList[tcbl.cursor]
	tcBlk.counter = tcBlk.counter + 1
	tcbl.cursor = tcbl.cursor - 1
	return tcBlk, nil
}

func (tcbl *tcertBlockListRoundRobin) GetUpdateNum() int {
	return tcbl.size - tcbl.len
}
