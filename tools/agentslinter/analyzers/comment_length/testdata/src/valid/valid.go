// Package valid is a package doc, which is the paragraph explaining a package rather than a
// declaration.
//
// A package comment may run long. It is the one place a package gets to explain what it is
// for at whatever length that takes, so no prefix sets the paragraph aside.
package valid

// A is a single sentence with a period.
func A() {}

// B is a single sentence with no period
func B() {}

// C names os.Getenv and a version like 1.5 and stays one sentence.
func C() {}

// D points at https://example.com/a/b.html for the reason.
func D() {}

// E lists things, e.g. this one and that one, in one sentence.
func E() {}

// J quotes a $(...) command substitution without ending the sentence there.
func J() {}

/* F is documented in a block comment. */
func F() {}

//go:generate echo hi
func G() {}

func H() {
	// Explainer: a free paragraph inside a function may run long. It stands on its own
	// rather than documenting a declaration, so it is allowed the room.
	_ = 1
}
