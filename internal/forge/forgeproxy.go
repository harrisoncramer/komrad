package forge

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"komrad/internal/store"
	"komrad/lib/xfile"
)

// ForgeProxyPlaceholder is the token git, gh and the server send through the forge proxy,
// which swaps it for the person's GitHub login.
const ForgeProxyPlaceholder = "komrad-forge-proxy"

// githubHost is the host the forge proxy connection is for.
const githubHost = "github.com"

// errNoViewer says a forge cannot say who its credential belongs to beyond a login.
var errNoViewer = errors.New("the forge cannot say who its credential belongs to")

// Viewer is who a credential belongs to, and the name and email they commit as.
type Viewer struct {
	Login string
	Name  string
	// Email is empty when the account keeps it private.
	Email string
	ID    int64
}

// viewerReader is a forge that can say who its credential belongs to.
type viewerReader interface {
	ReadViewer(ctx context.Context) (*Viewer, error)
}

// ConnectThroughForgeProxy asks GitHub through the forge proxy who the engine's GitHub login
// belongs to, and keeps github.com's connection holding the placeholder for that account.
func (s *Service) ConnectThroughForgeProxy(ctx context.Context) (*Viewer, error) {
	build, ok := s.builders[KindGitHub]
	if !ok {
		return nil, errUnknownKind.Saying("this build knows no GitHub")
	}

	probe := build("", staticCredential{Header: HeaderBearer, Token: NewSecret(ForgeProxyPlaceholder)})
	reader, ok := probe.(viewerReader)
	if !ok {
		return nil, errNoViewer
	}

	connection, err := s.connections.GetByHost(ctx, githubHost)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}

	viewer, err := reader.ReadViewer(ctx)
	if errors.Is(err, ErrUnauthorized) && connection != nil {
		return nil, errors.Join(err, s.connections.SetVerifiedAt(ctx, connection.ID, nil))
	}
	if err != nil {
		return nil, err
	}

	if connection == nil {
		if _, err := s.Connect(ctx, ConnectParams{Host: githubHost, Token: NewSecret(ForgeProxyPlaceholder)}); err != nil {
			return nil, err
		}

		return viewer, nil
	}

	if err := s.connections.SetLogin(ctx, connection.ID, viewer.Login, s.now()); err != nil {
		return nil, err
	}

	return viewer, nil
}

// WriteCommitIdentity writes the git config file naming who the engine's commits are by, and
// leaves it alone when it already says so.
func WriteCommitIdentity(path string, viewer Viewer) error {
	name := viewer.Name
	if strings.TrimSpace(name) == "" {
		name = viewer.Login
	}

	email := viewer.Email
	if strings.TrimSpace(email) == "" {
		email = strconv.FormatInt(viewer.ID, 10) + "+" + viewer.Login + "@users.noreply.github.com"
	}

	content := []byte(fmt.Sprintf("[user]\n\tname = %s\n\temail = %s\n", quoteGitConfig(name), quoteGitConfig(email)))

	current, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("failed to read the commit identity: %w", err)
	}
	if bytes.Equal(current, content) {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("failed to make the commit identity's directory: %w", err)
	}

	if err := xfile.WriteAtomic(path, content, 0o644); err != nil {
		return fmt.Errorf("failed to write the commit identity: %w", err)
	}

	return nil
}

// quoteGitConfig is a value as git config reads it back, whatever it holds.
func quoteGitConfig(value string) string {
	value = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == 0 {
			return ' '
		}

		return r
	}, value)

	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value) + `"`
}
