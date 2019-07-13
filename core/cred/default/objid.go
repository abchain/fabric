package cred_default

import (
	"encoding/asn1"
)

type idTreeNode struct {
	value interface{}
	child map[int]*idTreeNode
}

type ObjIdIndex struct {
	idTreeNode
}

func (i *idTreeNode) add(v interface{}, index []int) {

	if len(index) == 0 {
		i.value = v
	} else {
		if i.child == nil {
			i.child = make(map[int]*idTreeNode)
		}

		item := new(idTreeNode)
		i.child[index[0]] = item
		item.add(v, index[1:])
	}
}

func (i *idTreeNode) find(index []int) (interface{}, []int) {

	if len(index) == 0 {
		return i.value, nil
	}

	if i.child == nil {
		return i.value, index
	}

	if next, ok := i.child[index[0]]; ok {
		return next.find(index[1:])
	} else {
		return i.value, index
	}
}

func (i *ObjIdIndex) AddItem(v interface{}, objid asn1.ObjectIdentifier) {
	i.add(v, []int(objid))
}

func (i *ObjIdIndex) GetItem(objid asn1.ObjectIdentifier) (interface{}, bool, asn1.ObjectIdentifier) {
	v, res := i.find([]int(objid))

	isMatched := res == nil
	return v, isMatched, res
}
