package invalid

// Two sentences here. This is the second one. // want `comment must be a single sentence unless it is prefixed with "Explainer:"`
func A() {}

// The first sentence ends here. The second one has no period // want `comment must be a single sentence unless it is prefixed with "Explainer:"`
func B() {}

// This spans lines and says one thing. // want `comment must be a single sentence unless it is prefixed with "Explainer:"`
// Then it says another.
func C() {}

/* Block comments are caught too. Both of them. */ // want `comment must be a single sentence unless it is prefixed with "Explainer:"`
func D()                                           {}

// An explainer that is not at the start. Explainer: does not help here. // want `comment must be a single sentence unless it is prefixed with "Explainer:"`
func E() {}
