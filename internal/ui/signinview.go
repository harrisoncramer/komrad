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
	case accessGranted:
	}

	return ""
}
