// Package forge serves the connections komrad holds to the services pull requests live on, and
// reads pull requests through them.
package forge

import (
	"context"
	"errors"
	"time"

	"komrad/internal/fail"
	"komrad/internal/forge"
	"komrad/internal/rpcerr"
	forgestore "komrad/internal/store/forge"
	"komrad/lib/dates"
	forgev1 "komrad/proto/gen/forge/v1"

	"google.golang.org/protobuf/types/known/emptypb"
)

// connector is what the handler asks of the forge service: the connections, and the forge to
// speak to as one of them.
type connector interface {
	Connect(ctx context.Context, params forge.ConnectParams) (*forgestore.Connection, error)
	ListConnections(ctx context.Context) ([]forgestore.Connection, error)
	Disconnect(ctx context.Context, host string) error
	Check(ctx context.Context, connection forgestore.Connection) (string, error)
	HasCredential(ctx context.Context, connection forgestore.Connection) (bool, error)
	SetVerifiedAt(ctx context.Context, id string, at *time.Time) error
	ConnectionFor(ctx context.Context, host string) (*forgestore.Connection, error)
	ForConnection(connection forgestore.Connection) (forge.Forge, error)
}

// pullRequestWriter changes a pull request through the connection for its host.
type pullRequestWriter interface {
	MergePullRequest(ctx context.Context, address string) error
	SetPullRequestDraft(ctx context.Context, address string, draft bool) error
	SetPullRequestAutoMerge(ctx context.Context, params forge.SetAutoMergeParams) error
}

type Handler struct {
	forges connector
	writer pullRequestWriter
	now    func() time.Time
}

// NewHandler constructs the handler over the forge service and the pull request writer.
func NewHandler(forges connector, writer pullRequestWriter) *Handler {
	return &Handler{
		forges: forges,
		writer: writer,
		now:    time.Now,
	}
}

// ConnectForge proves a credential against the forge and records the connection.
func (s *Handler) ConnectForge(ctx context.Context, req *forgev1.ConnectForgeRequest) (*forgev1.ConnectForgeResponse, *fail.Error) {
	connection, err := s.forges.Connect(ctx, forge.ConnectParams{
		Host:   req.GetHost(),
		Kind:   req.GetKind(),
		APIURL: req.GetApiUrl(),
		Method: req.GetMethod(),
		Token:  forge.NewSecret(req.GetToken()),
	})
	if err != nil {
		return nil, rpcerr.From(err, "failed to connect the forge")
	}

	resource, err := s.render(ctx, *connection)
	if err != nil {
		return nil, rpcerr.From(err, "failed to read the connection back")
	}

	return &forgev1.ConnectForgeResponse{Forge: resource}, nil
}

// ListForges is every connection, by host.
func (s *Handler) ListForges(ctx context.Context, _ *emptypb.Empty) (*forgev1.ListForgesResponse, *fail.Error) {
	connections, err := s.forges.ListConnections(ctx)
	if err != nil {
		return nil, rpcerr.From(err, "failed to list forges")
	}

	resources := make([]*forgev1.Forge, 0, len(connections))
	for _, connection := range connections {
		resource, err := s.render(ctx, connection)
		if err != nil {
			return nil, rpcerr.From(err, "failed to read a connection")
		}

		resources = append(resources, resource)
	}

	return &forgev1.ListForgesResponse{Forges: resources}, nil
}

// CheckForges asks every forge whether its credential still works and records the answer.
func (s *Handler) CheckForges(ctx context.Context, _ *emptypb.Empty) (*forgev1.CheckForgesResponse, *fail.Error) {
	connections, err := s.forges.ListConnections(ctx)
	if err != nil {
		return nil, rpcerr.From(err, "failed to list forges")
	}

	checks := make([]*forgev1.ForgeCheck, 0, len(connections))
	for _, connection := range connections {
		check := &forgev1.ForgeCheck{}
		if _, err := s.forges.Check(ctx, connection); err != nil {
			check.Problem = err.Error()
			check.ProblemKind = problemKind(err)
			if check.ProblemKind != ProblemUnreachable {
				if err := s.forges.SetVerifiedAt(ctx, connection.ID, nil); err != nil {
					return nil, rpcerr.From(err, "failed to record the refusal")
				}

				connection.VerifiedAt = nil
			}
		} else {
			at := s.now()
			if err := s.forges.SetVerifiedAt(ctx, connection.ID, &at); err != nil {
				return nil, rpcerr.From(err, "failed to record the check")
			}

			connection.VerifiedAt = &at
		}

		resource, err := s.render(ctx, connection)
		if err != nil {
			return nil, rpcerr.From(err, "failed to read a connection")
		}

		check.Forge = resource
		checks = append(checks, check)
	}

	return &forgev1.CheckForgesResponse{Checks: checks}, nil
}

// DisconnectForge forgets a forge and the credential it was connected with.
func (s *Handler) DisconnectForge(ctx context.Context, req *forgev1.DisconnectForgeRequest) (*emptypb.Empty, *fail.Error) {
	if err := s.forges.Disconnect(ctx, req.GetHost()); err != nil {
		return nil, rpcerr.From(err, "failed to disconnect the forge")
	}

	return &emptypb.Empty{}, nil
}

// The kinds of trouble a check reports.
const (
	ProblemRefused      = "refused"
	ProblemNoCredential = "no_credential"
	ProblemUnreachable  = "unreachable"
)

// problemKind says which kind of trouble a connection is in.
func problemKind(err error) string {
	switch {
	case errors.Is(err, forge.ErrNoCredential):
		return ProblemNoCredential
	case errors.Is(err, forge.ErrUnauthorized), errors.Is(err, forge.ErrAppReachesNothing):
		return ProblemRefused
	default:
		return ProblemUnreachable
	}
}

func (s *Handler) render(ctx context.Context, connection forgestore.Connection) (*forgev1.Forge, error) {
	held, err := s.forges.HasCredential(ctx, connection)
	if err != nil {
		return nil, err
	}

	return &forgev1.Forge{
		Id:            connection.ID,
		Kind:          connection.Kind,
		Host:          connection.Host,
		ApiUrl:        connection.APIURL,
		Method:        connection.Method,
		Login:         connection.Login,
		VerifiedAt:    dates.StampPtr(connection.VerifiedAt),
		HasCredential: held,
		CreatedAt:     dates.Stamp(connection.CreatedAt),
	}, nil
}
