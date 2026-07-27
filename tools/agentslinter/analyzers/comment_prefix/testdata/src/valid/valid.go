package valid

// ExportedWithDoc does the thing.
func ExportedWithDoc() {}

func ExportedNoDoc() {}

// Do runs.
func Do() {}

// somethingElse describes an unexported func and is ignored.
func unexportedFn() {}

type T struct{}

// ExportedMethod does the thing.
func (t T) ExportedMethod() {}
