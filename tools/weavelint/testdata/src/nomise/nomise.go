package nomise

import "errors"

// start is what `mise run docker:up` brings up. // want `names the task runner`
const start = "docker"

var errNoDatabase = errors.New("nothing answered; run mise run docker:up") // want `names the task runner`

// promise is the word without the boundary, which the rule leaves alone.
var promise = "a premise, a compromise, a Miser, and MISE_ENV" // want `names the task runner`

func miseTask() string { // want `names the task runner`
	return promise + start
}
