package sharedfake

type paneStarter interface {
	Start(pane string) error
	Kill(pane string) error
}

type tmuxPanes struct{}

func (t tmuxPanes) Start(_ string) error {
	return nil
}

func (t tmuxPanes) Kill(_ string) error {
	return nil
}

var _ paneStarter = tmuxPanes{}
