package core

import "testing"

func newMemCore(t *testing.T) *Core {
	t.Helper()
	c, _ := New(Options{Store: openTempStore(t)})
	return c
}
