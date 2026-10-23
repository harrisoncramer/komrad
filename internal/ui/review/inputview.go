package review

// view renders the field, and nothing at all where there is no field open.
func (i inputField) view() string {
	if i.form == nil {
		return ""
	}

	return i.form.View()
}
