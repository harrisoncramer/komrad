package ui

type bindingTmux struct {
	globals   map[string]string
	bound     map[string][]string
	unbound   []string
	rootBinds int
}

func (t *bindingTmux) SetGlobal(name, value string) {
	if t.globals == nil {
		t.globals = map[string]string{}
	}

	t.globals[name] = value
}

func (t *bindingTmux) ShowGlobal(name string) string {
	return t.globals[name]
}

func (t *bindingTmux) GetSessionClient(string) string {
	return ""
}

func (t *bindingTmux) SetSessionOption(string, string, string) {}

func (t *bindingTmux) DetachSession(string) {}

func (t *bindingTmux) BindKey(key string, command ...string) {
	if t.bound == nil {
		t.bound = map[string][]string{}
	}

	t.bound[key] = command
}

func (t *bindingTmux) BindRootKey(key string, command ...string) {
	if t.bound == nil {
		t.bound = map[string][]string{}
	}

	t.bound["root:"+key] = command
	t.rootBinds++
}

func (t *bindingTmux) UnbindKey(key string) {
	delete(t.bound, key)

	t.unbound = append(t.unbound, key)
}

func (t *bindingTmux) UnbindRootKey(key string) {
	delete(t.bound, "root:"+key)

	t.unbound = append(t.unbound, key)
}
