package mapping

type row struct {
	name string
	tags []string
}

func name(r row) string {
	return r.name
}

func names(rows []row) []string {
	var out []string
	for _, r := range rows { // want `range loop only appends one value per element: write it as xslice.Map`
		out = append(out, r.name)
	}

	return out
}

func viaFunc(rows []row) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows { // want `range loop only appends one value per element`
		out = append(out, name(r))
	}

	return out
}

func intoField(rows []row) row {
	var r row
	for _, other := range rows { // want `range loop only appends one value per element`
		r.tags = append(r.tags, other.name)
	}

	return r
}

func byIndex(rows []row, picks []int) []row {
	var out []row
	for _, i := range picks {
		out = append(out, rows[i])
	}

	return out
}

func withIndex(rows []row) []int {
	var out []int
	for i := range rows {
		out = append(out, i)
	}

	return out
}

func usingIndex(rows []row) []string {
	var out []string
	for i, r := range rows {
		out = append(out, r.name+string(rune(i)))
	}

	return out
}

func filtered(rows []row) []string {
	var out []string
	for _, r := range rows {
		if r.name == "" {
			continue
		}

		out = append(out, r.name)
	}

	return out
}

func spread(rows []row) []string {
	var out []string
	for _, r := range rows {
		out = append(out, r.tags...)
	}

	return out
}

func overMap(rows map[string]row) []string {
	var out []string
	for _, r := range rows {
		out = append(out, r.name)
	}

	return out
}

func overString(s string) []rune {
	var out []rune
	for _, r := range s {
		out = append(out, r)
	}

	return out
}

func doubled(rows []row) []row {
	for _, r := range rows {
		rows = append(rows, r)
	}

	return rows
}

func twoStatements(rows []row) []string {
	var out []string
	for _, r := range rows {
		r.name += "!"
		out = append(out, r.name)
	}

	return out
}

func elsewhere(rows []row) []string {
	var out, other []string
	for _, r := range rows {
		other = append(out, r.name)
	}

	return other
}
