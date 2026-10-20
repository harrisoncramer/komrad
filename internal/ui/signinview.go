package ui

import "komrad/internal/errui"

func (m *model) signInView() string {
	width, url := m.term.width, m.settings.Addr

	switch m.access.stage {
	case accessChecking:
		return errui.RenderSignInChecking(width, url)
	case accessSignInNeeded, accessLoggingIn:
		return errui.RenderSignInNeeded(errui.RenderSignInNeededParams{
			Width:       width,
			URL:         url,
			AuthURL:     m.access.authURL,
			Err:         m.access.err,
			IsLoggingIn: m.access.stage == accessLoggingIn,
		})
	case accessDenied:
		return errui.RenderAccessDenied(width, url, m.access.err)
	case accessFailed:
		return errui.RenderAccessFailure(width, url, m.access.err)
	case accessStopped:
		return errui.RenderEngineStopped(width, url, m.access.err)
	case accessStarting:
		return errui.RenderEngineStarting(width, url)
	case accessGitHubNeeded, accessAuthorizingGitHub:
		params := errui.RenderGitHubNeededParams{
			Width:         width,
			URL:           url,
			Err:           m.access.err,
			IsAuthorizing: m.access.stage == accessAuthorizingGitHub,
		}
		if code := m.access.githubCode; code != nil {
			params.UserCode, params.VerificationURI = code.UserCode, code.VerificationURI
		}

		return errui.RenderGitHubNeeded(params)
	case accessGranted:
	}

	return ""
}
