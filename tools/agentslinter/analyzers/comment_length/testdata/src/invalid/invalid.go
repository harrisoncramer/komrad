// Package invalid is a package doc that reaches for the prefix it does not need. // want `a package comment is godoc output, so write the paragraph without the "Explainer:" prefix`
//
// Explainer: the prefix is godoc output here rather than a marker, so it reads as a typo on
// pkg.go.dev.
package invalid

// A has two sentences. This is the second one. // want `doc comment must be a single sentence`
func A() {}

// B ends its first sentence here. The second one has no period // want `doc comment must be a single sentence`
func B() {}

// C spans lines and says one thing. // want `doc comment must be a single sentence`
// Then it says another.
func C() {}

/* D is caught in a block comment too. Both sentences. */ // want `doc comment must be a single sentence`
func D()                                                  {}

// E says one thing up top. // want `doc comment must be a single sentence`
//
// A second paragraph is still part of the doc comment.
func E() {}

// F says one thing up top. // want `"Explainer:" is for the free paragraphs that explain a file, not for a doc comment`
//
// Explainer: a declaration does not get to set its reasoning aside. It is held to one
// sentence whatever it starts with.
func F() {}

type T struct {
	// Field is documented in two sentences. This is the second. // want `doc comment must be a single sentence`
	Field int
}

func G() {
	// A free paragraph is still one sentence by default. This one is not. // want `comment must be a single sentence unless the paragraph starts with "Explainer:"`
	_ = 1
}
