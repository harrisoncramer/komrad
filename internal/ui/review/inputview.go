package ui

// view renders the field, and nothing at all where there is no field open.
func (i reviewInput) view() string {
	if i.form == nil {
		return ""
	}

	return i.form.View()
}
