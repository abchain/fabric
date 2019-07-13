// +build !linux !static

package gorocksdb

// #cgo !windows LDFLAGS: -lrocksdb -lstdc++ -lm -lz -lbz2 -lsnappy
// #cgo windows LDFLAGS: -lstdc++
import "C"
