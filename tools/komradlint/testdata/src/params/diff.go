package params

type DiffParams struct {
	Base string
}

func Diff(p DiffParams) string {
	return p.Base
}
