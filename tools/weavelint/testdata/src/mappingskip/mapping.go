package mappingskip

func names(rows []string) []string {
	var out []string
	for _, r := range rows {
		out = append(out, r+"!")
	}

	return out
}
