package invalid

// bad comment that does not start with the name. // want `doc comment for exported function "ExportedFn" must begin with "ExportedFn"`
func ExportedFn() {}

// Doer starts with a longer word, not the identifier. // want `doc comment for exported function "Do" must begin with "Do"`
func Do() {}

type T struct{}

// wrong method comment. // want `doc comment for exported method "ExportedMethod" must begin with "ExportedMethod"`
func (t T) ExportedMethod() {}
