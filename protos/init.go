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

package protos

import (
	"crypto/rand"
	"encoding/binary"
	mrand "math/rand"
	"time"

	"github.com/op/go-logging"
)

var logger *logging.Logger

func init() {
	// Create package logger.
	logger = logging.MustGetLogger("protos")
}

func MustGetUUID() (uuid [16]byte) {
	_, err := rand.Read(uuid[:])
	if err != nil {

		logger.Warningf("Use weak uuid for error in random driver: %s", err)

		//use a uuid-1 like variant
		tm := time.Now()
		tmpart := tm.Unix()
		tmpartExt := tm.UnixNano()
		rnpart := mrand.Int63()

		binary.LittleEndian.PutUint64(uuid[:], uint64(tmpart))
		binary.BigEndian.PutUint64(uuid[8:], uint64(rnpart))
		binary.BigEndian.PutUint16(uuid[8:], uint16(tmpartExt))

		// variant-2 (microsoft) bits
		uuid[8] = uuid[8]&^0xe0 | 0xc0
		// version 1 (pseudo-random); see section 4.1.3
		uuid[6] = uuid[6]&^0xf0 | 0x10
	} else {
		// variant bits; see section 4.1.1
		uuid[8] = uuid[8]&^0xc0 | 0x80

		// version 4 (pseudo-random); see section 4.1.3
		uuid[6] = uuid[6]&^0xf0 | 0x40
	}
	return
}
