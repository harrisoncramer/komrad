package worktrees

import (
	"context"
	"errors"
	"sync"
	"time"

	"komrad/internal/git"
	worktreestore "komrad/internal/store/worktree"
)

type checkoutGit interface {
	ReadChanges(ctx context.Context, dir string) (git.Changes, error)
	PeekChanges(ctx context.Context, dir string) (git.Changes, error)
	ReadTreeState(ctx context.Context, dir string) (git.TreeState, error)
	IsSequenceInProgress(ctx context.Context, dir string) (bool, error)
	ReadBase(ctx context.Context, dir, base string) (*git.Base, error)
	FetchBase(ctx context.Context, dir, base string) (ref, sha string, err error)
	PredictRebase(ctx context.Context, dir, ref string) (git.Prediction, error)
}

// Checkout is where one directory stands against its base.
type Checkout struct {
	Changes git.Changes
	// BaseRef is what the branch is measured against, empty when git cannot read the directory
	// or the clone does not hold the base.
	BaseRef string
	// BaseErr is why the base could not be named, when it could not.
	BaseErr            error
	SequenceInProgress bool
	// Landed is how many of the branch's own commits the base already holds whole.
	Landed int
	// Unlanded is how many of the branch's own commits the base does not yet hold.
	Unlanded int
	// Unrelated says the branch shares no history with the base.
	Unrelated bool
}

type prediction struct {
	key       string
	landed    int
	unlanded  int
	unrelated bool
}

type NewCheckoutReaderParams struct {
	Git checkoutGit
	// Now is the clock, time.Now when unset.
	Now func() time.Time
}

// CheckoutReader reads where a directory stands against its base.
type CheckoutReader struct {
	git         checkoutGit
	now         func() time.Time
	predictedMu sync.Mutex
	predicted   map[string]prediction
}

// NewCheckoutReader builds a checkout reader over git.
func NewCheckoutReader(params NewCheckoutReaderParams) *CheckoutReader {
	now := params.Now
	if now == nil {
		now = time.Now
	}

	return &CheckoutReader{
		git:       params.Git,
		now:       now,
		predicted: map[string]prediction{},
	}
}

type ReadStandingParams struct {
	Dir      string
	Readable bool
	// Base is the base already read for the directory, nil when BaseErr says why it could not be.
	Base    *git.Base
	BaseErr error
	HeadSHA string
}

// ReadStanding reads where a directory stands against its base without asking origin or taking
// the index lock, or why git could not say.
func (r *CheckoutReader) ReadStanding(ctx context.Context, params ReadStandingParams) worktreestore.Standing {
	var checkout Checkout
	var err error
	if params.Readable {
		err = r.readAgainst(ctx, &checkout, readAgainstParams{
			Dir:     params.Dir,
			Base:    params.Base,
			BaseErr: params.BaseErr,
			Peek:    true,
			HeadSHA: params.HeadSHA,
		})
	}

	return r.standingOf(standingOfParams{
		Dir:      params.Dir,
		Readable: params.Readable,
		Checkout: checkout,
		Err:      err,
	})
}

// ReadBase is the ref a directory is measured against and the commit it points at, as the clone
// last fetched it.
func (r *CheckoutReader) ReadBase(ctx context.Context, dir, base string) (*git.Base, error) {
	return r.git.ReadBase(ctx, dir, base)
}

type readParams struct {
	Dir   string
	Base  string
	Fetch bool
}

func (r *CheckoutReader) readCheckout(ctx context.Context, checkout *Checkout, params readParams) error {
	base, err := r.readBase(ctx, params)

	return r.readAgainst(ctx, checkout, readAgainstParams{
		Dir:     params.Dir,
		Base:    base,
		BaseErr: err,
	})
}

type readAgainstParams struct {
	Dir     string
	Base    *git.Base
	BaseErr error
	// Peek reads the checkout without writing the index.
	Peek    bool
	HeadSHA string
}

// readAgainst reads where the directory stands against a base already read.
func (r *CheckoutReader) readAgainst(ctx context.Context, checkout *Checkout, params readAgainstParams) error {
	readTree := r.readTree
	if params.Peek {
		readTree = r.peekTree
	}

	if errors.Is(params.BaseErr, git.ErrBaseMissing) {
		checkout.BaseErr = params.BaseErr

		return readTree(ctx, params.Dir, checkout)
	}
	if params.BaseErr != nil {
		return params.BaseErr
	}
	checkout.BaseRef = params.Base.Ref

	if err := readTree(ctx, params.Dir, checkout); err != nil {
		return err
	}

	predicted, err := r.predictRebase(ctx, params)
	if err != nil {
		return err
	}
	checkout.Landed, checkout.Unlanded, checkout.Unrelated = predicted.landed, predicted.unlanded, predicted.unrelated

	return nil
}

func (r *CheckoutReader) predictRebase(ctx context.Context, params readAgainstParams) (prediction, error) {
	var key string
	if params.HeadSHA != "" {
		key = params.HeadSHA + ":" + params.Base.Tip
	}

	r.predictedMu.Lock()
	last, ok := r.predicted[params.Dir]
	r.predictedMu.Unlock()
	if key != "" && ok && last.key == key {
		return last, nil
	}

	predicted := prediction{key: key}
	rebase, err := r.git.PredictRebase(ctx, params.Dir, params.Base.Ref)
	switch {
	case errors.Is(err, git.ErrBaseUnrelated):
		predicted.unrelated = true
	case err != nil:
		return prediction{}, err
	default:
		predicted.landed, predicted.unlanded = rebase.Landed, rebase.Replay
	}

	if key != "" {
		r.predictedMu.Lock()
		r.predicted[params.Dir] = predicted
		r.predictedMu.Unlock()
	}

	return predicted, nil
}

// readBase is the base the branch is measured against, fetched from origin first when asked.
func (r *CheckoutReader) readBase(ctx context.Context, params readParams) (*git.Base, error) {
	if !params.Fetch {
		return r.git.ReadBase(ctx, params.Dir, params.Base)
	}

	ref, sha, err := r.git.FetchBase(ctx, params.Dir, params.Base)
	if err != nil {
		return nil, err
	}

	return &git.Base{Ref: ref, Tip: sha}, nil
}

// readTree reads whether a sequence is part-way through and what is uncommitted, refreshing the
// index first.
func (r *CheckoutReader) readTree(ctx context.Context, dir string, checkout *Checkout) error {
	state, err := r.git.ReadTreeState(ctx, dir)
	if err != nil {
		return err
	}
	checkout.SequenceInProgress = state.InProgress

	checkout.Changes, err = r.git.ReadChanges(ctx, dir)

	return err
}

// peekTree reads what readTree does without writing the index.
func (r *CheckoutReader) peekTree(ctx context.Context, dir string, checkout *Checkout) error {
	inProgress, err := r.git.IsSequenceInProgress(ctx, dir)
	if err != nil {
		return err
	}
	checkout.SequenceInProgress = inProgress

	checkout.Changes, err = r.git.PeekChanges(ctx, dir)

	return err
}

type standingOfParams struct {
	Dir      string
	Readable bool
	Checkout Checkout
	// Err is why the reading failed, when it did.
	Err error
}

// standingOf is what a reading says of where the directory stands.
func (r *CheckoutReader) standingOf(params standingOfParams) worktreestore.Standing {
	checkout := params.Checkout
	standing := worktreestore.Standing{
		Dir:        params.Dir,
		ReadAt:     r.now(),
		Dirty:      len(checkout.Changes.Modified)+len(checkout.Changes.Untracked) > 0,
		Sequencing: checkout.SequenceInProgress,
		Unlanded:   checkout.Unlanded,
	}

	switch {
	case params.Err != nil:
		standing.Error = storableText(params.Err.Error())
	case !params.Readable:
		standing.Error = "git cannot read the directory"
	case checkout.BaseErr != nil:
		standing.Error = storableText(checkout.BaseErr.Error())
	case checkout.Unrelated:
		standing.Error = "the branch shares no history with " + checkout.BaseRef
	}

	return standing
}
