package inline

func stub() error {
	return fail.NotFound("a test's own error answers nobody, so the rule leaves it alone")
}
