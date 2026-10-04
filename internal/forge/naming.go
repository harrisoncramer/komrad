package forge

import "strconv"

// Ref names a pull request the way its forge writes it, such as #12 on GitHub or !12 on GitLab.
func Ref(kind string, number int) string {
	if kind == KindGitLab {
		return "!" + strconv.Itoa(number)
	}

	return "#" + strconv.Itoa(number)
}

// Noun is what a forge of the kind calls a pull request.
func Noun(kind string) string {
	if kind == KindGitLab {
		return "merge request"
	}

	return "pull request"
}

// Abbrev is the short form a forge of the kind gives a pull request.
func Abbrev(kind string) string {
	if kind == KindGitLab {
		return "mr"
	}

	return "pr"
}

// Name is what a forge of the kind is called, or the forge itself when the kind is unknown.
func Name(kind string) string {
	switch kind {
	case KindGitHub:
		return "GitHub"
	case KindGitLab:
		return "GitLab"
	}

	return "the forge"
}
