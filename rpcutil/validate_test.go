package rpcutil

import (
	. "testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/validator.v2"
)

func TestArrMap(t *T) {
	InstallCustomValidators()

	tags := "max=5,arrMap=max=5,arrMap=min=1,min=1"
	require.NotNil(t, validator.Valid([]interface{}{}, tags))
	require.NotNil(t, validator.Valid([]interface{}{""}, tags))
	require.Nil(t, validator.Valid([]interface{}{"ohai", 3}, tags))
	require.NotNil(t, validator.Valid([]interface{}{10}, tags))
}

func TestPreRegex(t *T) {
	InstallCustomValidators()
	RegisterRegex("int", "^[0-9]+$")
	RegisterRegex("alpha", "^[A-Za-z]+$")

	require.NotNil(t, validator.Valid("something 10", "preRegex=int"))
	require.NotNil(t, validator.Valid("something 10", "preRegex=alpha"))
	require.Nil(t, validator.Valid("10", "preRegex=int"))
	require.Nil(t, validator.Valid("Something", "preRegex=alpha"))
}

func TestLens(t *T) {
	InstallCustomValidators()

	tags := "lens=2|0"
	require.Nil(t, validator.Valid(int64(2), tags))
	require.Nil(t, validator.Valid(int64(0), tags))
	require.NotNil(t, validator.Valid(int64(9), tags))
	require.Nil(t, validator.Valid([]int64{5, 6}, tags))
	require.Nil(t, validator.Valid([]int64{}, tags))
	require.NotNil(t, validator.Valid([]int64{1}, tags))
	require.Nil(t, validator.Valid("hi", tags))
	require.Nil(t, validator.Valid("", tags))
	require.NotNil(t, validator.Valid("hello", tags))
	require.Nil(t, validator.Valid(float64(2.0), tags))
	require.Nil(t, validator.Valid(float64(0), tags))
	require.NotNil(t, validator.Valid(float64(2.1), tags))
}
