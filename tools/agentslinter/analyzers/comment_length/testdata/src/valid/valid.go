package valid

// Single sentence with a period.
func A() {}

// Single sentence with no period
func B() {}

// A sentence naming os.Getenv and a version like 1.5 stays one sentence.
func C() {}

// See https://example.com/a/b.html for the reason.
func D() {}

// A list of things, e.g. this one and that one, is still one sentence.
func E() {}

// Explainer: this one is allowed to run long. It has a second sentence. It even
// has a third, because the prefix says it is deliberate.
func F() {}

/* Block comments count the same way. */
func G() {}

//go:generate echo hi
func H() {}
