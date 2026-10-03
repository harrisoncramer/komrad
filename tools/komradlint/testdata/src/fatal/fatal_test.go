package fatal

import (
	"errors"
	"testing"
)

type helper struct {
	t *testing.T
}

func TestFatal(t *testing.T) {
	err := errors.New("boom")

	t.Fatal(err)                     // want `a test fails on an error through require.NoError or assert.NoError, not t.Fatal or t.Error`
	t.Fatalf("failed: %v", err)      // want `a test fails on an error through require.NoError or assert.NoError, not t.Fatal or t.Error`
	t.Error(err)                     // want `a test fails on an error through require.NoError or assert.NoError, not t.Fatal or t.Error`
	t.Errorf("failed: %v", err)      // want `a test fails on an error through require.NoError or assert.NoError, not t.Fatal or t.Error`
	helper{t: t}.t.Fatalf("%v", err) // want `a test fails on an error through require.NoError or assert.NoError, not t.Fatal or t.Error`

	t.Fatal("no error here")
	t.Errorf("got %d", 3)
	t.Errorf("failed: %s", err.Error())
	t.Log(err)
}

func BenchmarkFatal(b *testing.B) {
	b.Fatal(errors.New("boom")) // want `a test fails on an error through require.NoError or assert.NoError, not t.Fatal or t.Error`
}

func fail(tb testing.TB, err error) {
	tb.Fatalf("failed: %v", err) // want `a test fails on an error through require.NoError or assert.NoError, not t.Fatal or t.Error`
}

type reporter struct{}

func (reporter) Fatal(args ...any) {}

func TestOther(t *testing.T) {
	reporter{}.Fatal(errors.New("boom"))
}
