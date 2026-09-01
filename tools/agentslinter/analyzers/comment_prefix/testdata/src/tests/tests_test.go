package tests

import "testing"

// TestDocumented pins the thing the test is there to pin.
func TestDocumented(t *testing.T) {}

func TestUndocumented(t *testing.T) {} // want `test "TestUndocumented" needs a doc comment beginning with "TestUndocumented"`

// covers something, but does not start with the name. // want `doc comment for test "TestMisnamed" must begin with "TestMisnamed"`
func TestMisnamed(t *testing.T) {}

// BenchmarkDocumented measures the thing.
func BenchmarkDocumented(b *testing.B) {}

type fake struct{}

func (fake) ExportedStub() {}

func helper(t *testing.T) {}

func Testing(t *testing.T) {}

func Exampleish() {}
