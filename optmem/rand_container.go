package optmem

import "math/rand"

// randContainer is a container for *rand.Rands to be safely used concurrently
// by multiple users.
//
// This is basically a free-list for random sources.
type randContainer struct {
	r chan *rand.Rand
}

// newRandContainer creates a new randContainer.
//
// It will buffer bufLen entries..
func newRandContainer(f func() *rand.Rand, bufLen uint) *randContainer {
	toReturn := &randContainer{
		r: make(chan *rand.Rand, bufLen),
	}

	for i := uint(0); i < bufLen; i++ {
		toReturn.Put(f())
	}

	return toReturn
}

func (c *randContainer) Get() *rand.Rand {
	return <-c.r
}

func (c *randContainer) Put(r *rand.Rand) {
	c.r <- r
}
