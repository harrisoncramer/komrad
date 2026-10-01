package pullrequests

import (
	"sync"
	"time"

	"komrad/internal/forge"
	"komrad/internal/git"
	pullrequeststore "komrad/internal/store/pullrequest"
	"komrad/lib/xptr"
)

// hotGap is how long after a read a repository likely to change soon is read again.
const hotGap = 5 * time.Second

// coldGap is how long after a read any other repository is read again.
const coldGap = time.Minute

// pendingHotWithin is how long after the forge last stamped a change a pull request with pending
// checks keeps its repository hot.
const pendingHotWithin = 20 * time.Minute

// blankHotWithin is how long after the forge first answered a blank mergeability for a head the
// pull request keeps its repository hot.
const blankHotWithin = 2 * time.Minute

// movedHotWithin is how long a repository stays hot after its agents' branches or heads moved,
// or after komrad wrote to a pull request on its host.
const movedHotWithin = 2 * time.Minute

// paceSlack is how far short of its gap a repository is still due.
const paceSlack = time.Second

// pacing is when each repository was last read and how soon it is read again.
type pacing struct {
	mu           sync.Mutex
	repositories map[string]*repositoryPace
	// heated is when komrad last wrote to a pull request on each host.
	heated map[string]time.Time
	// slowed is when each host's budget comes back after it ran low.
	slowed map[string]time.Time
	// woken says every repository is due on the next pass.
	woken bool
}

// repositoryPace is where one repository stands in the pacing.
type repositoryPace struct {
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
		repositories: map[string]*repositoryPace{},
		heated:       map[string]time.Time{},
		slowed:       map[string]time.Time{},
	}
}

// candidate is one repository live agents sit on, as a pass finds it.
type candidate struct {
	repositoryURL string
	remote        git.RemoteURL
	// branches is every branch its live agents sit on.
	branches []string
	// fingerprint is those branches with each agent's head.
	fingerprint string
	// askedAt is when the pass that found the repository due started.
	askedAt time.Time
}

// pick is the repositories due now, consuming a wake.
func (p *pacing) pick(candidates []candidate, now time.Time) []candidate {
	p.mu.Lock()
	defer p.mu.Unlock()

	woken := p.woken
	p.woken = false

	due := []candidate{}
	for _, next := range candidates {
		if woken || p.isDue(next, now) {
			next.askedAt = now
			due = append(due, next)
		}
	}

	return due
}

// isDue says whether a repository is read on this pass.
func (p *pacing) isDue(next candidate, now time.Time) bool {
	last, ok := p.repositories[next.repositoryURL]
	if !ok || last.fingerprint != next.fingerprint {
		return true
	}

	return now.Sub(last.readAt) >= p.gapFor(last, next.remote.Host, now)-paceSlack
}

// gapFor is how long after its last read a repository is read again.
func (p *pacing) gapFor(last *repositoryPace, host string, now time.Time) time.Duration {
	if now.Before(p.slowed[host]) {
		return coldGap
	}

	hot := now.Before(last.hotUntil) ||
		now.Sub(last.movedAt) < movedHotWithin ||
		now.Sub(p.heated[host]) < movedHotWithin
	if hot {
		return hotGap
	}

	return coldGap
}

// read records a repository having been read, hot until the moment the forge's answer gave.
func (p *pacing) read(repository candidate, hotUntil time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()

	last, ok := p.repositories[repository.repositoryURL]
	if !ok {
		last = &repositoryPace{}
		p.repositories[repository.repositoryURL] = last
	}
	if ok && last.fingerprint != repository.fingerprint {
		last.movedAt = repository.askedAt
	}

	last.readAt = repository.askedAt
	last.fingerprint = repository.fingerprint
	last.hotUntil = hotUntil
}

// fail records a repository whose read failed as read and cold.
func (p *pacing) fail(repository candidate) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.repositories[repository.repositoryURL] = &repositoryPace{readAt: repository.askedAt, fingerprint: repository.fingerprint}
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

// hotUntil is how long a pull request keeps its repository hot.
func hotUntil(pullRequest forge.PullRequest, change *pullrequeststore.StandingChange) time.Time {
	if pullRequest.IsFinished() {
		return time.Time{}
	}

	until := time.Time{}
	if xptr.Deref(pullRequest.Checks) == forge.ChecksPending {
		until = pullRequest.UpdatedAt.Add(pendingHotWithin)
	}
	if change.BlankSince != nil {
		if blank := change.BlankSince.Add(blankHotWithin); blank.After(until) {
			until = blank
		}
	}

	return until
}
