package sharedfake

type fakePanes struct { // want `fakePanes fakes sharedfake.paneStarter, which tmuxtest.Fake already fakes; use that instead`
	started []string
}

func (f *fakePanes) Start(pane string) error {
	f.started = append(f.started, pane)

	return nil
}

func (f *fakePanes) Kill(_ string) error {
	return nil
}

type starterOnly struct{}

func (s starterOnly) Start(_ string) error {
	return nil
}

type anyStarter interface {
	Start(pane string) error
	Kill(pane string) error
}
