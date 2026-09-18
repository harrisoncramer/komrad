package quiet

import "weave/internal/forge"

func leak(s forge.Secret) string {
	return s.Expose()
}
