package errctx

import (
	"errors"
	. "testing"

	"github.com/stretchr/testify/assert"
)

type key int

func TestErrCtx(t *T) {
	err := errors.New("foo")

	assert.Equal(t, err, Base(err))

	err1 := Set(err, key(0), "a")
	assert.Equal(t, err.Error(), err1.Error())
	assert.Equal(t, err, Base(err1))
	assert.Nil(t, Get(err, key(0)))
	assert.Equal(t, "a", Get(err1, key(0)))

	err2 := Set(err, key(1), "b")
	assert.NotEqual(t, err1, err2)
	assert.Equal(t, err.Error(), err2.Error())
	assert.Equal(t, err, Base(err2))
	assert.Nil(t, Get(err, key(1)))
	assert.Nil(t, Get(err2, key(0)))
	assert.Equal(t, "b", Get(err2, key(1)))

	err3 := Set(err2, key(2), "c")
	assert.Equal(t, err.Error(), err3.Error())
	assert.Equal(t, err, Base(err3))
	assert.Nil(t, Get(err3, key(0)))
	assert.Nil(t, Get(err2, key(2)))
	assert.Equal(t, "b", Get(err3, key(1)))
	assert.Equal(t, "c", Get(err3, key(2)))
}
