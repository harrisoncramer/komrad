package required

// ExportedWithDoc does the thing.
func ExportedWithDoc() {}

func ExportedNoDoc() {} // want `exported function "ExportedNoDoc" needs a doc comment beginning with "ExportedNoDoc"`

func unexportedNoDoc() {}

type T struct{}

func (t T) ExportedMethodNoDoc() {} // want `exported method "ExportedMethodNoDoc" needs a doc comment beginning with "ExportedMethodNoDoc"`

type unexported struct{}

func (unexported) ExportedStub() {}

// exportedStubWithBadComment is a method on an unexported type, still held to the shape. // want `doc comment for exported method "ExportedStubWithComment" must begin with "ExportedStubWithComment"`
func (unexported) ExportedStubWithComment() {}
