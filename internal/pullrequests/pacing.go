package pullrequests

import (
	"sync"
	"time"
)

// hotGap is how long after a read a repository likely to change soon is read again.
const hotGap = 5 * time.Second

// coldGap is how long after a read any other repository is read again.
const coldGap = time.Minute

// pendingHotWithin is how long after the forge last stamped a change a pull request with pending
// checks keeps its repository hot, so a required check that never reports cannot hold it there.
const pendingHotWithin = 20 * time.Minute

// blankHotWithin is how long after the forge first answered a blank mergeability for a head the
// pull request keeps its repository hot.
const blankHotWithin = 2 * time.Minute

// movedHotWithin is how long a repository stays hot after its agents' branches or heads moved,
// or after komrad wrote to a pull request on its host.
const movedHotWithin = 2 * time.Minute

// paceSlack is how far short of its gap a repository is still due, so a pass that starts a moment
// before the gap is up reads it rather than leaving it a whole tick.
const paceSlack = time.Second

// pacing is when each repository was last read and how soon it is read again, shared between the
// sync's loop and the writes that heat a host.
type pacing struct {
	mu           sync.Mutex
	repositories map[string]*pace
	// heated is when komrad last wrote to a pull request on each host.
	heated map[string]time.Time
	// slowed is when each host's budget comes back after it ran low.
	slowed map[string]time.Time
	// woken says every repository is due on the next pass.
	woken bool
}

// pace is where one repository stands in the pacing.
type pace struct {
	readAt time.Time
	// fingerprint is the branches and heads of the repository's agents when it was last read.
	fingerprint string
	// movedAt is when the fingerprint last changed.
	movedAt time.Time
	// hotUntil is how long what the forge last answered keeps the repository hot.
	hotUntil time.Time
}

func newPacing() *pacing {
	return &pacing{
		repositories: map[string]*pace{},
		heated:       map[string]time.Time{},
		slowed:       map[string]time.Time{},
	}
}

// dueParams is one repository asked about at the start of a pass.
type dueParams struct {
	repositoryURL string
	host          string
	fingerprint   string
	// askedAt is when the pass that found the repository due started.
	askedAt time.Time
}

// pick is the repositories due now, consuming a wake.
func (p *pacing) pick(candidates []dueParams, now time.Time) []dueParams {
	p.mu.Lock()
	defer p.mu.Unlock()

	woken := p.woken
	p.woken = false

	due := []dueParams{}
	for _, candidate := range candidates {
		if woken || p.isDue(candidate, now) {
			candidate.askedAt = now
			due = append(due, candidate)
		}
	}

	return due
}

func (p *pacing) isDue(params dueParams, now time.Time) bool {
	read, ok := p.repositories[params.repositoryURL]
	if !ok || read.fingerprint != params.fingerprint {
		return true
	}

	gap := coldGap
	if p.isHot(read, params.host, now) && !now.Before(p.slowed[params.host]) {
		gap = hotGap
	}

	return now.Sub(read.readAt) >= gap-paceSlack
}

func (p *pacing) isHot(read *pace, host string, now time.Time) bool {
	return now.Before(read.hotUntil) ||
		now.Sub(read.movedAt) < movedHotWithin ||
		now.Sub(p.heated[host]) < movedHotWithin
}

// readParams is one repository having been read.
type readParams struct {
	repositoryURL string
	fingerprint   string
	hotUntil      time.Time
	// askedAt is when the pass that read it started, which the next gap is measured from.
	askedAt time.Time
}

// read records a repository having been read.
func (p *pacing) read(params readParams) {
	p.mu.Lock()
	defer p.mu.Unlock()

	read, ok := p.repositories[params.repositoryURL]
	if !ok {
		read = &pace{}
		p.repositories[params.repositoryURL] = read
	}
	if ok && read.fingerprint != params.fingerprint {
		read.movedAt = params.askedAt
	}

	read.readAt = params.askedAt
	read.fingerprint = params.fingerprint
	read.hotUntil = params.hotUntil
}

// fail records a repository whose read failed as read now and cold, so a repository the forge
// will not answer is asked again a minute later rather than on every pass.
func (p *pacing) fail(params dueParams) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.repositories[params.repositoryURL] = &pace{readAt: params.askedAt, fingerprint: params.fingerprint}
}

// heat keeps every repository on the host hot for a while from now.
func (p *pacing) heat(host string, now time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.heated[host] = now
}

// slow keeps every repository on the host in the slow lane until the moment.
func (p *pacing) slow(host string, until time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.slowed[host] = until
}

// wake makes every repository due on the next pass.
func (p *pacing) wake() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.woken = true
}
