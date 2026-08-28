package excluded

// A has two sentences. This file is excluded by path, so neither is reported.
func A() {}

/* B has another one. And its second sentence. */
func B() {}
