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

package discovery

import (
	"github.com/abchain/fabric/core/db"
	pb "github.com/abchain/fabric/protos"
	"github.com/golang/protobuf/proto"
	"math/rand"
	"sync"
	"time"
)

// Discovery is the interface that consolidates bootstrap peer membership
// selection and validating peer selection for non-validating peers
type Discovery interface {
	AddNode(string) bool           // Add an address to the discovery list
	ResumeNode(string)             // Add or resume existed address from discovery list
	RemoveNode(string) bool        // Remove an address from the discovery list
	AddNodes([]string) []string    // Add a batch of address, and return which is added
	GetAllNodes() []string         // Return all addresses this peer maintains
	GetRandomNode() string         // Return n random addresses for this peer to connect t
	GetRandomNodes(n int) []string // Return n random addresses for this peer to connect to
	FindNode(string) bool          // Find a node in the discovery list
	DiscoveryPersistor
}

type DiscoveryPersistor interface {
	LoadDiscoveryList(db.PersistorKey) error
	StoreDiscoveryList(db.PersistorKey) error
}

// DiscoveryImpl is an implementation of Discovery
type DiscoveryImpl struct {
	sync.RWMutex
	nodes  map[string]bool
	seq    []string
	random *rand.Rand
}

// NewDiscoveryImpl is a constructor of a Discovery implementation
func NewDiscoveryImpl() *DiscoveryImpl {
	di := DiscoveryImpl{}
	di.nodes = make(map[string]bool)
	di.random = rand.New(rand.NewSource(time.Now().Unix()))
	return &di
}

// AddNode adds an address to the discovery list
func (di *DiscoveryImpl) AddNode(address string) bool {
	di.Lock()
	defer di.Unlock()
	if _, ok := di.nodes[address]; !ok {
		di.seq = append(di.seq, address)
		di.nodes[address] = true
	}
	return di.nodes[address]
}

func (di *DiscoveryImpl) ResumeNode(address string) {
	di.Lock()
	defer di.Unlock()
	di.nodes[address] = true
}

func (di *DiscoveryImpl) AddNodes(addrs []string) []string { // Add a batch of address, and return which is added

	di.Lock()
	defer di.Unlock()

	seqBefore := len(di.seq)

	for _, addr := range addrs {
		if _, ok := di.nodes[addr]; !ok {
			di.seq = append(di.seq, addr)
			di.nodes[addr] = true
		}
	}

	return di.seq[seqBefore:]

}

// RemoveNode removes an address from the discovery list
func (di *DiscoveryImpl) RemoveNode(address string) bool {
	di.Lock()
	defer di.Unlock()
	di.nodes[address] = false
	return true
}

// GetAllNodes returns an array of all addresses saved in the discovery list
func (di *DiscoveryImpl) GetAllNodes() []string {
	di.RLock()
	defer di.RUnlock()
	var addresses []string
	for address, valid := range di.nodes {
		if valid {
			addresses = append(addresses, address) // TODO Expensive, don't quite like it
		}
	}
	return addresses
}

func (di *DiscoveryImpl) GetRandomNode() string {
	di.RLock()
	defer di.RUnlock()
	if nodeslen := len(di.nodes); nodeslen > 0 {
		return di.seq[di.random.Intn(nodeslen)]
	} else {
		return ""
	}
}

// GetRandomNodes returns n random nodes
// YA-fabric 0.9: it has a stupid implement so now it just return 1 random node
func (di *DiscoveryImpl) GetRandomNodes(n int) []string {
	return []string{di.GetRandomNode()}
}

// FindNode returns true if its address is stored in the discovery list
func (di *DiscoveryImpl) FindNode(address string) bool {
	di.RLock()
	defer di.RUnlock()
	_, ok := di.nodes[address]
	return ok
}

func inArray(element string, array []string) bool {
	for _, val := range array {
		if val == element {
			return true
		}
	}
	return false
}

// StoreDiscoveryList enables a peer to persist the discovery list to the database
func (di *DiscoveryImpl) StoreDiscoveryList(k db.PersistorKey) error {

	persistor := db.NewPersistor(k)
	var err error
	addresses := di.GetAllNodes()
	raw, err := proto.Marshal(&pb.PeersAddresses{Addresses: addresses})
	if err != nil {
		return err
	}
	return persistor.Store("discovery", raw)
}

// LoadDiscoveryList enables a peer to load the discovery list from the database
func (di *DiscoveryImpl) LoadDiscoveryList(k db.PersistorKey) error {

	persistor := db.NewPersistor(k)
	var err error
	packed, err := persistor.Load("discovery")
	if err != nil {
		return err
	}
	addresses := &pb.PeersAddresses{}
	err = proto.Unmarshal(packed, addresses)
	if err != nil {
		return err
	}
	di.Lock()
	defer di.Unlock()
	for _, address := range addresses.GetAddresses() {
		di.seq = append(di.seq, address)
		di.nodes[address] = true
	}
	return nil

}
