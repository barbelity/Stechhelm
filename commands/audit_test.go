package commands

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestSimpleHello(t *testing.T) {
	assert.True(t, true)
}

//
//func TestComplexHello(t *testing.T) {
//	conf := &helloConfiguration{
//		addressee: "World",
//		repeat:    3,
//		shout:     true,
//		prefix:    "test: ",
//	}
//	assert.Equal(t, doGreet(conf), "TEST: HELLO WORLD!\nTEST: HELLO WORLD!\nTEST: HELLO WORLD!")
//}
