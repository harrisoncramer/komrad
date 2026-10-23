package forge

import (
	"strconv"

	forgev1 "komrad/proto/gen/forge/v1"
)

// Ref names a pull request the way its forge writes it, such as #12 on GitHub or !12 on GitLab.
func Ref(kind forgev1.Kind, number int) string {
	if kind == forgev1.Kind_KIND_GITLAB {
		return "!" + strconv.Itoa(number)
	}

	return "#" + strconv.Itoa(number)
}

// Noun is what a forge of the kind calls a pull request.
func Noun(kind forgev1.Kind) string {
	if kind == forgev1.Kind_KIND_GITLAB {
		return "merge request"
	}

	return "pull request"
}

// Abbrev is the short form a forge of the kind gives a pull request.
func Abbrev(kind forgev1.Kind) string {
	if kind == forgev1.Kind_KIND_GITLAB {
		return "mr"
	}

	return "pr"
}

// Name is what a forge of the kind is called, or the forge itself when the kind is unknown.
func Name(kind forgev1.Kind) string {
	switch kind {
	case forgev1.Kind_KIND_GITHUB:
		return "GitHub"
	case forgev1.Kind_KIND_GITLAB:
		return "GitLab"
	case forgev1.Kind_KIND_UNSPECIFIED:
	}

	return "the forge"
}
