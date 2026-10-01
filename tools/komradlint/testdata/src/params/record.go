package params

type RecordParams struct {
	Name string
}

type DiffParams struct {
	Base string
}

func (e *Engine) Diff(params DiffParams) string {
	return params.Base
}
