package agent

import (
	"context"
	"testing"
	"time"

	"komrad/internal/caller"
	"komrad/internal/documents"
	"komrad/internal/store"
	documentstore "komrad/internal/store/document"
	documenttypestore "komrad/internal/store/documenttype"
	"komrad/lib/dates"
	"komrad/lib/xptr"
	agentv1 "komrad/proto/gen/agent/v1"
	"komrad/proto/gen/agent/v1/agentv1komrad"

	"connectrpc.com/connect"
	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/emptypb"
)

const documentID = "01a0d1d7-0000-7000-8000-0000000000d0"

const documentTypeID = "01a0d1d7-0000-7000-8000-0000000000e0"

// fakeDocuments answers from its fields and records what each call asked of it.
type fakeDocuments struct {
	err     error
	stored  documentstore.Document
	written []documents.WriteDocumentParams
	listed  []fakeDocumentsList
	// versions is every write of stored, and read the versions a get asked for.
	versions     []documentstore.Version
	read         []*int32
	types        []documenttypestore.Type
	writtenTypes []documents.WriteDocumentTypeParams
	deletedTypes []string
}

type fakeDocumentsList struct {
	AgentID string
	Type    *string
}

// WriteDocument records the write and answers stored with the written fields unless err refuses it.
func (f *fakeDocuments) WriteDocument(_ context.Context, params documents.WriteDocumentParams) (*documentstore.Document, error) {
	f.written = append(f.written, params)
	if f.err != nil {
		return nil, f.err
	}

	written := f.stored
	written.AgentID = params.AgentID
	written.Name = params.Name
	written.Type = params.Type
	written.Body = params.Body

	return &written, nil
}

// GetDocument records the version asked for and answers stored unless err refuses it.
func (f *fakeDocuments) GetDocument(_ context.Context, _ string, version *int32) (*documentstore.Document, error) {
	f.read = append(f.read, version)
	if f.err != nil {
		return nil, f.err
	}
	got := f.stored

	return &got, nil
}

// ListDocuments records whose documents were asked for and answers stored without its body.
func (f *fakeDocuments) ListDocuments(_ context.Context, agentID string, typeName *string) ([]documentstore.Document, error) {
	f.listed = append(f.listed, fakeDocumentsList{AgentID: agentID, Type: typeName})
	if f.err != nil {
		return nil, f.err
	}
	listed := f.stored
	listed.Body = ""

	return []documentstore.Document{listed}, nil
}

// ListDocumentVersions answers versions as the writes of stored unless err refuses it.
func (f *fakeDocuments) ListDocumentVersions(_ context.Context, _ string) (string, []documentstore.Version, error) {
	if f.err != nil {
		return "", nil, f.err
	}

	return f.stored.AgentID, f.versions, nil
}

// ListDocumentTypes answers types unless err refuses it.
func (f *fakeDocuments) ListDocumentTypes(_ context.Context) ([]documenttypestore.Type, error) {
	return f.types, f.err
}

// GetDocumentType answers the first of types unless err refuses it.
func (f *fakeDocuments) GetDocumentType(_ context.Context, _ string) (*documenttypestore.Type, error) {
	if f.err != nil {
		return nil, f.err
	}
	got := f.types[0]

	return &got, nil
}

// WriteDocumentType records the write and answers it as stored unless err refuses it.
func (f *fakeDocuments) WriteDocumentType(_ context.Context, params documents.WriteDocumentTypeParams) (*documenttypestore.Type, error) {
	f.writtenTypes = append(f.writtenTypes, params)
	if f.err != nil {
		return nil, f.err
	}

	return &documenttypestore.Type{ID: documentTypeID, Name: params.Name, Context: params.Context, Version: 1}, nil
}

// DeleteDocumentType records the delete unless err refuses it.
func (f *fakeDocuments) DeleteDocumentType(_ context.Context, id string) error {
	f.deletedTypes = append(f.deletedTypes, id)

	return f.err
}

func newDocumentHandler(docs *fakeDocuments) agentv1komrad.AgentServiceAdapter {
	return agentv1komrad.AgentServiceAdapter{Impl: NewHandler(NewHandlerParams{Documents: docs})}
}

// TestDocumentErrors pins which connect code each refusal of a document call becomes.
func TestDocumentErrors(t *testing.T) {
	write := func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
		_, err := svc.WriteDocument(ctx, connect.NewRequest(&agentv1.WriteDocumentRequest{
			Name:   "Documents",
			Type:   "RFC",
			Source: &agentv1.WriteDocumentRequest_Body{Body: "# Documents"},
		}))

		return err
	}
	writeForAnother := func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
		_, err := svc.WriteDocument(ctx, connect.NewRequest(&agentv1.WriteDocumentRequest{
			AgentId: xptr.Ptr(betaID),
			Name:    "Documents",
			Type:    "RFC",
			Source:  &agentv1.WriteDocumentRequest_Body{Body: "# Documents"},
		}))

		return err
	}
	list := func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
		_, err := svc.ListDocuments(ctx, connect.NewRequest(&agentv1.ListDocumentsRequest{}))

		return err
	}
	get := func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
		_, err := svc.GetDocument(ctx, connect.NewRequest(&agentv1.GetDocumentRequest{Id: documentID}))

		return err
	}
	versions := func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
		_, err := svc.ListDocumentVersions(ctx, connect.NewRequest(&agentv1.ListDocumentVersionsRequest{Id: documentID}))

		return err
	}
	getType := func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
		_, err := svc.GetDocumentType(ctx, connect.NewRequest(&agentv1.GetDocumentTypeRequest{Name: "postmortem"}))

		return err
	}
	writeType := func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
		_, err := svc.WriteDocumentType(ctx, connect.NewRequest(&agentv1.WriteDocumentTypeRequest{
			Id:      xptr.Ptr(documentTypeID),
			Version: xptr.Ptr(int32(1)),
			Name:    "postmortem",
			Context: "What broke and why.",
		}))

		return err
	}
	deleteType := func(ctx context.Context, svc agentv1komrad.AgentServiceAdapter) error {
		_, err := svc.DeleteDocumentType(ctx, connect.NewRequest(&agentv1.DeleteDocumentTypeRequest{Id: documentTypeID}))

		return err
	}

	tests := []struct {
		name     string
		call     func(context.Context, agentv1komrad.AgentServiceAdapter) error
		err      error
		asNobody bool
		wantCode connect.Code
	}{
		{
			name:     "creating a document with no author",
			call:     write,
			err:      documents.ErrNoAuthor,
			wantCode: connect.CodeInvalidArgument,
		},
		{
			name:     "an agent writing a document for another agent",
			call:     writeForAnother,
			wantCode: connect.CodeInvalidArgument,
		},
		{
			name:     "writing a name the agent already uses",
			call:     write,
			err:      store.ErrNameTaken,
			wantCode: connect.CodeAlreadyExists,
		},
		{
			name:     "writing a body over the cap",
			call:     write,
			err:      documents.ErrTooLarge,
			wantCode: connect.CodeInvalidArgument,
		},
		{
			name:     "writing from a path that cannot be read",
			call:     write,
			err:      documents.ErrUnreadablePath,
			wantCode: connect.CodeInvalidArgument,
		},
		{
			name:     "rewriting a document that does not exist",
			call:     write,
			err:      store.ErrNotFound,
			wantCode: connect.CodeNotFound,
		},
		{
			name:     "rewriting a document with no version",
			call:     write,
			err:      documents.ErrNoVersion,
			wantCode: connect.CodeInvalidArgument,
		},
		{
			name:     "rewriting a document written since it was read",
			call:     write,
			err:      documentstore.ErrChanged,
			wantCode: connect.CodeFailedPrecondition,
		},
		{
			name:     "listing with no id and no caller identity",
			call:     list,
			asNobody: true,
			wantCode: connect.CodeInvalidArgument,
		},
		{
			name:     "getting a document that does not exist",
			call:     get,
			err:      store.ErrNotFound,
			wantCode: connect.CodeNotFound,
		},
		{
			name:     "listing the versions of a document that does not exist",
			call:     versions,
			err:      store.ErrNotFound,
			wantCode: connect.CodeNotFound,
		},
		{
			name:     "writing a document as a type that does not exist",
			call:     write,
			err:      documentstore.ErrUnknownType,
			wantCode: connect.CodeInvalidArgument,
		},
		{
			name:     "getting a type that does not exist",
			call:     getType,
			err:      documentstore.ErrUnknownType,
			wantCode: connect.CodeInvalidArgument,
		},
		{
			name:     "naming a type a name another type has",
			call:     writeType,
			err:      store.ErrNameTaken,
			wantCode: connect.CodeAlreadyExists,
		},
		{
			name:     "rewriting a type with no version",
			call:     writeType,
			err:      documents.ErrNoTypeVersion,
			wantCode: connect.CodeInvalidArgument,
		},
		{
			name:     "rewriting a type written since it was read",
			call:     writeType,
			err:      documenttypestore.ErrChanged,
			wantCode: connect.CodeFailedPrecondition,
		},
		{
			name:     "deleting a type documents are written as",
			call:     deleteType,
			err:      documenttypestore.ErrInUse,
			wantCode: connect.CodeFailedPrecondition,
		},
		{
			name:     "deleting a type that does not exist",
			call:     deleteType,
			err:      store.ErrNotFound,
			wantCode: connect.CodeNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newDocumentHandler(&fakeDocuments{err: tt.err})

			ctx := t.Context()
			if !tt.asNobody {
				ctx = caller.With(ctx, caller.Caller{AgentID: alphaID})
			}

			err := tt.call(ctx, svc)
			assert.Equal(t, tt.wantCode, connect.CodeOf(err))
			if tt.err != nil {
				require.ErrorIs(t, err, tt.err)
			}
		})
	}
}

// TestWriteDocument pins that a write is the caller's own and comes back without its body.
func TestWriteDocument(t *testing.T) {
	tests := []struct {
		name     string
		req      *agentv1.WriteDocumentRequest
		asNobody bool
		want     documents.WriteDocumentParams
	}{
		{
			name: "a body creates a document",
			req: &agentv1.WriteDocumentRequest{
				Name:   "Documents",
				Type:   "RFC",
				Source: &agentv1.WriteDocumentRequest_Body{Body: "# Documents"},
			},
			want: documents.WriteDocumentParams{
				AgentID: alphaID,
				Name:    "Documents",
				Type:    "RFC",
				Body:    "# Documents",
			},
		},
		{
			name: "a path with an id rewrites that document",
			req: &agentv1.WriteDocumentRequest{
				Id:      xptr.Ptr(documentID),
				Version: xptr.Ptr(int32(2)),
				Name:    "Findings",
				Type:    "report",
				Source:  &agentv1.WriteDocumentRequest_Path{Path: "rfcs/findings.md"},
			},
			want: documents.WriteDocumentParams{
				AgentID: alphaID,
				ID:      xptr.Ptr(documentID),
				Version: xptr.Ptr(int32(2)),
				Name:    "Findings",
				Type:    "report",
				Path:    "rfcs/findings.md",
			},
		},
		{
			name: "a caller that is not an agent rewrites the document the id names",
			req: &agentv1.WriteDocumentRequest{
				Id:     xptr.Ptr(documentID),
				Name:   "Findings",
				Type:   "report",
				Source: &agentv1.WriteDocumentRequest_Body{Body: "edited"},
			},
			asNobody: true,
			want: documents.WriteDocumentParams{
				ID:   xptr.Ptr(documentID),
				Name: "Findings",
				Type: "report",
				Body: "edited",
			},
		},
		{
			name: "a caller that is not an agent creates a document for the agent it names",
			req: &agentv1.WriteDocumentRequest{
				AgentId: xptr.Ptr(betaID),
				Name:    "Findings",
				Type:    "report",
				Source:  &agentv1.WriteDocumentRequest_Body{Body: "Empty report."},
			},
			asNobody: true,
			want: documents.WriteDocumentParams{
				For:  betaID,
				Name: "Findings",
				Type: "report",
				Body: "Empty report.",
			},
		},
		{
			name: "an agent naming itself writes its own",
			req: &agentv1.WriteDocumentRequest{
				AgentId: xptr.Ptr(alphaID),
				Name:    "Documents",
				Type:    "RFC",
				Source:  &agentv1.WriteDocumentRequest_Body{Body: "# Documents"},
			},
			want: documents.WriteDocumentParams{
				AgentID: alphaID,
				For:     alphaID,
				Name:    "Documents",
				Type:    "RFC",
				Body:    "# Documents",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			docs := &fakeDocuments{stored: documentstore.Document{ID: documentID}}
			svc := newDocumentHandler(docs)

			ctx := t.Context()
			if !tt.asNobody {
				ctx = caller.With(ctx, caller.Caller{AgentID: alphaID})
			}

			written, err := svc.WriteDocument(ctx, connect.NewRequest(tt.req))
			require.NoError(t, err)

			assert.Equal(t, []documents.WriteDocumentParams{tt.want}, docs.written)
			assert.Equal(t, documentID, written.Msg.GetDocument().GetId())
			assert.Empty(t, written.Msg.GetDocument().GetBody())
		})
	}
}

// TestListDocuments pins whose documents a listing reads and when that read is recorded.
func TestListDocuments(t *testing.T) {
	tests := []struct {
		name       string
		req        *agentv1.ListDocumentsRequest
		wantListed fakeDocumentsList
		wantTarget string
	}{
		{
			name:       "no id lists the caller's own",
			req:        &agentv1.ListDocumentsRequest{},
			wantListed: fakeDocumentsList{AgentID: alphaID},
		},
		{
			name:       "a type narrows the listing",
			req:        &agentv1.ListDocumentsRequest{Type: xptr.Ptr("notes")},
			wantListed: fakeDocumentsList{AgentID: alphaID, Type: xptr.Ptr("notes")},
		},
		{
			name:       "another agent's documents are a recorded read",
			req:        &agentv1.ListDocumentsRequest{AgentId: xptr.Ptr(betaID)},
			wantListed: fakeDocumentsList{AgentID: betaID},
			wantTarget: betaID,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			docs := &fakeDocuments{stored: documentstore.Document{
				ID:      documentID,
				AgentID: tt.wantListed.AgentID,
				Body:    "hidden",
			}}
			svc := newDocumentHandler(docs)
			ctx, written := newLoggedCaller(t, alphaID)

			listed, err := svc.ListDocuments(ctx, connect.NewRequest(tt.req))
			require.NoError(t, err)

			assert.Equal(t, []fakeDocumentsList{tt.wantListed}, docs.listed)
			require.Len(t, listed.Msg.GetDocuments(), 1)
			assert.Empty(t, listed.Msg.GetDocuments()[0].GetBody())
			assertCrossAgentRead(t, assertCrossAgentReadParams{
				Written: written.String(),
				Body:    "agent listed another agent's documents",
				Target:  tt.wantTarget,
			})
		})
	}
}

// TestGetDocument pins that a document comes back whole and that reading another agent's is recorded.
func TestGetDocument(t *testing.T) {
	stored := documentstore.Document{
		ID:        documentID,
		AgentID:   betaID,
		Name:      "Documents",
		Type:      "explainer",
		Body:      "# How documents work",
		CreatedAt: time.Date(2026, time.September, 26, 9, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, time.September, 26, 10, 0, 0, 0, time.UTC),
	}

	tests := []struct {
		name       string
		from       string
		version    *int32
		wantTarget string
	}{
		{name: "the author reads its own", from: betaID},
		{
			name:       "another agent's read is recorded",
			from:       alphaID,
			wantTarget: betaID,
		},
		{
			name:    "a version reads that write",
			from:    betaID,
			version: xptr.Ptr(int32(2)),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			docs := &fakeDocuments{stored: stored}
			svc := newDocumentHandler(docs)
			ctx, written := newLoggedCaller(t, tt.from)

			got, err := svc.GetDocument(ctx, connect.NewRequest(&agentv1.GetDocumentRequest{Id: documentID, Version: tt.version}))
			require.NoError(t, err)

			assert.Equal(t, []*int32{tt.version}, docs.read)

			assert.Empty(t, cmp.Diff(&agentv1.Document{
				Id:        documentID,
				AgentId:   betaID,
				Name:      "Documents",
				Type:      "explainer",
				Body:      "# How documents work",
				CreatedAt: "2026-09-26T09:00:00Z",
				UpdatedAt: "2026-09-26T10:00:00Z",
			}, got.Msg.GetDocument(), protocmp.Transform()))
			assertCrossAgentRead(t, assertCrossAgentReadParams{
				Written: written.String(),
				Body:    "agent read another agent's document",
				Target:  tt.wantTarget,
			})
		})
	}
}

// TestListDocumentVersions pins that a document's writes come back as the wire draws them and that
// listing another agent's is recorded.
func TestListDocumentVersions(t *testing.T) {
	tests := []struct {
		name       string
		from       string
		wantTarget string
	}{
		{name: "the author lists its own", from: betaID},
		{
			name:       "another agent's listing is recorded",
			from:       alphaID,
			wantTarget: betaID,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newDocumentHandler(&fakeDocuments{
				stored: documentstore.Document{ID: documentID, AgentID: betaID},
				versions: []documentstore.Version{
					{
						Version:   2,
						Name:      "Documents",
						Type:      "RFC",
						ByPerson:  true,
						CreatedAt: time.Date(2026, time.September, 26, 10, 0, 0, 0, time.UTC),
					},
					{
						Version:   1,
						Name:      "Draft",
						Type:      "notes",
						CreatedAt: time.Date(2026, time.September, 26, 9, 0, 0, 0, time.UTC),
					},
				},
			})
			ctx, written := newLoggedCaller(t, tt.from)

			listed, err := svc.ListDocumentVersions(ctx, connect.NewRequest(&agentv1.ListDocumentVersionsRequest{Id: documentID}))
			require.NoError(t, err)

			assert.Empty(t, cmp.Diff([]*agentv1.DocumentVersion{
				{
					Version:   2,
					Name:      "Documents",
					Type:      "RFC",
					ByPerson:  true,
					CreatedAt: "2026-09-26T10:00:00Z",
				},
				{
					Version:   1,
					Name:      "Draft",
					Type:      "notes",
					CreatedAt: "2026-09-26T09:00:00Z",
				},
			}, listed.Msg.GetVersions(), protocmp.Transform()))
			assertCrossAgentRead(t, assertCrossAgentReadParams{
				Written: written.String(),
				Body:    "agent listed the versions of another agent's document",
				Target:  tt.wantTarget,
			})
		})
	}
}

// TestWriteDocumentType pins what a write of a type sends the service and renders back.
func TestWriteDocumentType(t *testing.T) {
	tests := []struct {
		name string
		req  *agentv1.WriteDocumentTypeRequest
		want documents.WriteDocumentTypeParams
	}{
		{
			name: "no id creates a type",
			req:  &agentv1.WriteDocumentTypeRequest{Name: "postmortem", Context: "What broke and why."},
			want: documents.WriteDocumentTypeParams{Name: "postmortem", Context: "What broke and why."},
		},
		{
			name: "an id and version rewrite that type",
			req: &agentv1.WriteDocumentTypeRequest{
				Id:      xptr.Ptr(documentTypeID),
				Version: xptr.Ptr(int32(3)),
				Name:    "incident",
				Context: "What broke and why.",
			},
			want: documents.WriteDocumentTypeParams{
				ID:      xptr.Ptr(documentTypeID),
				Version: xptr.Ptr(int32(3)),
				Name:    "incident",
				Context: "What broke and why.",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			docs := &fakeDocuments{}
			svc := newDocumentHandler(docs)

			written, err := svc.WriteDocumentType(t.Context(), connect.NewRequest(tt.req))
			require.NoError(t, err)

			assert.Equal(t, []documents.WriteDocumentTypeParams{tt.want}, docs.writtenTypes)
			assert.Equal(t, documentTypeID, written.Msg.GetDocumentType().GetId())
			assert.Equal(t, tt.want.Context, written.Msg.GetDocumentType().GetContext())
		})
	}
}

// TestListDocumentTypes pins how the types are rendered.
func TestListDocumentTypes(t *testing.T) {
	updated := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	docs := &fakeDocuments{types: []documenttypestore.Type{
		{ID: documentTypeID, Name: "RFC", Context: "A proposal.", Version: 2, UpdatedAt: updated},
	}}
	svc := newDocumentHandler(docs)

	listed, err := svc.ListDocumentTypes(caller.With(t.Context(), caller.Caller{AgentID: alphaID}), connect.NewRequest(&emptypb.Empty{}))
	require.NoError(t, err)

	want := []*agentv1.DocumentType{{
		Id:        documentTypeID,
		Name:      "RFC",
		Context:   "A proposal.",
		Version:   2,
		UpdatedAt: dates.PreciseStamp(updated),
	}}
	assert.Empty(t, cmp.Diff(want, listed.Msg.GetDocumentTypes(), protocmp.Transform()))
}
