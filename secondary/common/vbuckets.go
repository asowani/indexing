package common

import (
	"sort"
)

// Vbuckets is temporary data type that can be used to sort list of uint16
type Vbuckets []uint16

// Len implements sort.Interface{}.
func (vbuckets Vbuckets) Len() int {
	return len(vbuckets)
}

// Less implements sort.Interface{}.
func (vbuckets Vbuckets) Less(i, j int) bool {
	return vbuckets[i] < vbuckets[j]
}

// Swap implements sort.Interface{}
func (vbuckets Vbuckets) Swap(i, j int) {
	vbuckets[i], vbuckets[j] = vbuckets[j], vbuckets[i]
}

// To32 converts 16-bit vbucket values to 32-bit values,
// normally used for protobuf.
func (vbuckets Vbuckets) To32() []uint32 {
	vbnos := make([]uint32, len(vbuckets))
	for i, vbno := range vbuckets {
		vbnos[i] = uint32(vbno)
	}
	return vbnos
}

// Intersection operation on two sets of vbuckets, return a sorted list of
// vbuckets present in both set.
func Intersection(this, other []uint16) []uint16 {
	vbuckets := make([]uint16, 0, 32)
	cache := make(map[uint16]bool)
	for _, vbno := range this {
		cache[vbno] = true
	}
	for _, vbno := range other {
		if _, ok := cache[vbno]; ok {
			vbuckets = append(vbuckets, vbno)
		}
	}
	return vbuckets
}

// Union set operation on two sets of vbuckets, return a sorted list of
// vbuckets present in atleast one set.
func Union(this, other []uint16) []uint16 {
	vbuckets := make([]uint16, 0, 32)
	cache := make(map[uint16]bool)
	for _, vbno := range this {
		cache[vbno] = true
	}
	vbuckets = append(vbuckets, this...)
	for _, vbno := range other {
		if _, ok := cache[vbno]; ok == false {
			vbuckets = append(vbuckets, vbno)
		}
	}
	sort.Sort(Vbuckets(vbuckets))
	return vbuckets
}

// Vbno32to16 converts vbucket type from uint32 to uint16
func Vbno32to16(vbnos []uint32) []uint16 {
	vbnos16 := make([]uint16, 0, len(vbnos))
	for _, vb := range vbnos {
		vbnos16 = append(vbnos16, uint16(vb))
	}
	return vbnos16
}

// Vbno16to32 converts vbucket type from uint16 to uint32
func Vbno16to32(vbnos []uint16) []uint32 {
	vbnos32 := make([]uint32, 0, len(vbnos))
	for _, vb := range vbnos {
		vbnos32 = append(vbnos32, uint32(vb))
	}
	return vbnos32
}
