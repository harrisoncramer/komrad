package documents_test

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"komrad/internal/documents"
	"komrad/internal/events/eventstest"
	"komrad/internal/mail"
	"komrad/internal/notices"
	"komrad/internal/store"
	agentstore "komrad/internal/store/agent"
	documentstore "komrad/internal/store/document"
	mailstore "komrad/internal/store/mail"
	"komrad/internal/storetest"
	"komrad/lib/xptr"
)

// TestWrite pins where a body comes from and which bodies are refused.
func TestWrite(t *testing.T) {
	stores := storetest.Open(t)
	stores.SeedDocumentTypes(t, "notes", "report")
	cwd := t.TempDir()
	author := stores.SeedAgent(t, agentstore.Agent{Name: "author", Cwd: cwd})
	service := newService(stores, eventstest.NewQueue(t, stores, documents.NewEventHandler()))

	writeFile := func(name, content string) string {
		path := filepath.Join(cwd, name)
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

		return path
	}
	require.NoError(t, os.Mkdir(filepath.Join(cwd, "folder"), 0o700))
	require.NoError(t, syscall.Mkfifo(filepath.Join(cwd, "pipe"), 0o600))

	tests := []struct {
		name     string
		params   documents.WriteDocumentParams
		wantErr  error
		wantBody string
	}{
		{
			name:     "a body is stored as given",
			params:   documents.WriteDocumentParams{Name: "given", Body: "# Given"},
			wantBody: "# Given",
		},
		{
			name:     "an absolute path is read in",
			params:   documents.WriteDocumentParams{Name: "absolute", Path: writeFile("absolute.md", "# Absolute")},
			wantBody: "# Absolute",
		},
		{
			name:     "a relative path is read from the agent's working directory",
			params:   documents.WriteDocumentParams{Name: "relative", Path: filepath.Base(writeFile("relative.md", "# Relative"))},
			wantBody: "# Relative",
		},
		{
			name:    "a path that names nothing is refused",
			params:  documents.WriteDocumentParams{Name: "missing", Path: "missing.md"},
			wantErr: documents.ErrUnreadablePath,
		},
		{
			name:    "a path naming a directory is refused",
			params:  documents.WriteDocumentParams{Name: "folder", Path: "folder"},
			wantErr: documents.ErrUnreadablePath,
		},
		{
			name:    "a path naming a pipe is refused without waiting on a writer",
			params:  documents.WriteDocumentParams{Name: "pipe", Path: "pipe"},
			wantErr: documents.ErrUnreadablePath,
		},
		{
			name:    "a path naming an empty file is refused as empty",
			params:  documents.WriteDocumentParams{Name: "blank", Path: writeFile("blank.md", "")},
			wantErr: documents.ErrEmptyFile,
		},
		{
			name:    "a file over the cap is refused before it is read",
			params:  documents.WriteDocumentParams{Name: "huge", Path: writeFile("huge.md", strings.Repeat("a", documents.MaxBodyBytes+1))},
			wantErr: documents.ErrTooLarge,
		},
		{
			name:    "a body over the cap is refused",
			params:  documents.WriteDocumentParams{Name: "huge", Body: strings.Repeat("a", documents.MaxBodyBytes+1)},
			wantErr: documents.ErrTooLarge,
		},
		{
			name:    "a body that is not UTF-8 is refused",
			params:  documents.WriteDocumentParams{Name: "binary", Path: writeFile("binary.md", "\xff\xfe")},
			wantErr: documents.ErrNotUTF8,
		},
		{
			name:    "neither a body nor a path is refused",
			params:  documents.WriteDocumentParams{Name: "empty"},
			wantErr: documents.ErrNoBody,
		},
		{
			name:    "a type that does not exist is refused",
			params:  documents.WriteDocumentParams{Name: "untyped", Type: "postmortem", Body: "# Untyped"},
			wantErr: documentstore.ErrUnknownType,
		},
		{
			name: "an agent that does not exist is not found",
			params: documents.WriteDocumentParams{
				AgentID: uuid.NewString(),
				Name:    "orphan",
				Body:    "# Orphan",
			},
			wantErr: store.ErrNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := tt.params
			if params.AgentID == "" {
				params.AgentID = author.ID
			}
			if params.Type == "" {
				params.Type = "notes"
			}

			written, err := service.WriteDocument(t.Context(), params)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)

				return
			}
			require.NoError(t, err)

			stored, err := stores.Documents.Get(t.Context(), written.ID)
			require.NoError(t, err)
			assert.Equal(t, tt.wantBody, stored.Body)
		})
	}
}

// TestWriteWithAnIDRewritesThatDocument pins that a write naming an id changes that document rather than adding one.
func TestWriteWithAnIDRewritesThatDocument(t *testing.T) {
	stores := storetest.Open(t)
	author := stores.SeedAgent(t, agentstore.Agent{Name: "author"})
	service := newService(stores, eventstest.NewQueue(t, stores, documents.NewEventHandler()))
	ctx := t.Context()

	draft, err := service.WriteDocument(ctx, documents.WriteDocumentParams{
		AgentID: author.ID,
		Name:    "Documents",
		Type:    "RFC",
		Body:    "draft",
	})
	require.NoError(t, err)

	revised, err := service.WriteDocument(ctx, documents.WriteDocumentParams{
		AgentID: author.ID,
		ID:      xptr.Ptr(draft.ID),
		Version: xptr.Ptr(draft.Version),
		Name:    "Documents RFC",
		Type:    "RFC",
		Body:    "revised",
	})
	require.NoError(t, err)
	assert.Equal(t, draft.ID, revised.ID)

	listed, err := service.ListDocuments(ctx, author.ID, nil)
	require.NoError(t, err)
	require.Len(t, listed, 1, "a retitled document is the same document")
	assert.Equal(t, "Documents RFC", listed[0].Name)
}

// TestReadAnEarlierWrite pins that a document reads as an earlier write left it, and that a write
// or document nobody made is not found.
func TestReadAnEarlierWrite(t *testing.T) {
	stores := storetest.Open(t)
	stores.SeedDocumentTypes(t, "notes", "report")
	author := stores.SeedAgent(t, agentstore.Agent{Name: "author"})
	service := newService(stores, eventstest.NewQueue(t, stores, documents.NewEventHandler()))
	ctx := t.Context()

	draft, err := service.WriteDocument(ctx, documents.WriteDocumentParams{
		AgentID: author.ID,
		Name:    "Draft",
		Type:    "notes",
		Body:    "draft",
	})
	require.NoError(t, err)
	_, err = service.WriteDocument(ctx, documents.WriteDocumentParams{
		AgentID: author.ID,
		ID:      xptr.Ptr(draft.ID),
		Version: xptr.Ptr(draft.Version),
		Name:    "Documents",
		Type:    "RFC",
		Body:    "revised",
	})
	require.NoError(t, err)

	first, err := service.GetDocument(ctx, draft.ID, xptr.Ptr(int32(1)))
	require.NoError(t, err)
	assert.Equal(t, draft.ID, first.ID)
	assert.Equal(t, author.ID, first.AgentID)
	assert.Equal(t, "Draft", first.Name)
	assert.Equal(t, "notes", first.Type)
	assert.Equal(t, "draft", first.Body)
	assert.Equal(t, int32(1), first.Version)

	current, err := service.GetDocument(ctx, draft.ID, nil)
	require.NoError(t, err)
	assert.Equal(t, "revised", current.Body)

	_, err = service.GetDocument(ctx, draft.ID, xptr.Ptr(int32(3)))
	require.ErrorIs(t, err, store.ErrNotFound)
	_, _, err = service.ListDocumentVersions(ctx, uuid.NewString())
	require.ErrorIs(t, err, store.ErrNotFound)
}

// TestWriteByAPerson pins that a write by no agent rewrites the document its id names, or
// creates one for the agent it is for, and leaves it with that agent.
func TestWriteByAPerson(t *testing.T) {
	stores := storetest.Open(t)
	stores.SeedDocumentTypes(t, "notes", "report")
	author := stores.SeedAgent(t, agentstore.Agent{Name: "author"})
	other := stores.SeedAgent(t, agentstore.Agent{Name: "other"})
	service := newService(stores, eventstest.NewQueue(t, stores, documents.NewEventHandler()))
	ctx := t.Context()

	draft, err := service.WriteDocument(ctx, documents.WriteDocumentParams{
		AgentID: author.ID,
		Name:    "Documents",
		Type:    "RFC",
		Body:    "draft",
	})
	require.NoError(t, err)

	tests := []struct {
		name      string
		params    documents.WriteDocumentParams
		wantErr   error
		wantBody  string
		wantOwner string
	}{
		{
			name: "an id rewrites that document",
			params: documents.WriteDocumentParams{
				ID:      xptr.Ptr(draft.ID),
				Version: xptr.Ptr(draft.Version),
				Name:    "Documents",
				Type:    "RFC",
				Body:    "edited",
			},
			wantBody: "edited",
		},
		{
			name: "a rewrite naming no version is refused",
			params: documents.WriteDocumentParams{
				ID:   xptr.Ptr(draft.ID),
				Name: "Documents",
				Type: "RFC",
				Body: "blind",
			},
			wantErr: documents.ErrNoVersion,
		},
		{
			name: "an id naming an agent other than its owner is refused",
			params: documents.WriteDocumentParams{
				ID:      xptr.Ptr(draft.ID),
				Version: xptr.Ptr(draft.Version),
				For:     other.ID,
				Name:    "Documents",
				Type:    "RFC",
				Body:    "moved",
			},
			wantErr: documents.ErrForAnother,
		},
		{
			name: "no id and no agent it is for is refused",
			params: documents.WriteDocumentParams{
				Name: "Fresh",
				Type: "RFC",
				Body: "new",
			},
			wantErr: documents.ErrNoAuthor,
		},
		{
			name: "no id creates a document for the agent it is for",
			params: documents.WriteDocumentParams{
				For:  author.ID,
				Name: "Fresh",
				Type: "RFC",
				Body: "Empty RFC.",
			},
			wantBody: "Empty RFC.",
		},
		{
			name: "no id creates a document owned by the agent it is for",
			params: documents.WriteDocumentParams{
				For:  other.ID,
				Name: "Report",
				Type: "report",
				Body: "Report.",
			},
			wantBody:  "Report.",
			wantOwner: other.ID,
		},
		{
			name: "an agent it is for that does not exist is not found",
			params: documents.WriteDocumentParams{
				For:  uuid.NewString(),
				Name: "Stray",
				Type: "RFC",
				Body: "stray",
			},
			wantErr: store.ErrNotFound,
		},
		{
			name: "an id no document holds is not found",
			params: documents.WriteDocumentParams{
				ID:      xptr.Ptr(uuid.NewString()),
				Version: xptr.Ptr(int32(1)),
				Name:    "Lost",
				Type:    "RFC",
				Body:    "lost",
			},
			wantErr: store.ErrNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			written, err := service.WriteDocument(ctx, tt.params)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)

				return
			}
			require.NoError(t, err)

			stored, err := stores.Documents.Get(ctx, written.ID)
			require.NoError(t, err)
			assert.Equal(t, tt.wantBody, stored.Body)
			wantOwner := author.ID
			if tt.wantOwner != "" {
				wantOwner = tt.wantOwner
			}
			assert.Equal(t, wantOwner, stored.AgentID)

			_, versions, err := service.ListDocumentVersions(ctx, written.ID)
			require.NoError(t, err)
			assert.True(t, versions[0].ByPerson, "the write is kept as a person's")
		})
	}
}

// TestAPersonsWriteMailsTheAuthor pins which writes tell the author.
func TestAPersonsWriteMailsTheAuthor(t *testing.T) {
	stores := storetest.Open(t)
	queue := eventstest.NewQueue(t, stores, documents.NewEventHandler())
	service := newService(stores, queue)
	ctx := t.Context()

	tests := []struct {
		name     string
		archived bool
		full     bool
		byPerson bool
		creates  bool
		then     func(t *testing.T, author string, written *documentstore.Document) *documentstore.Document
		wantMail string
	}{
		{
			name: "the author's own rewrite sends nothing",
		},
		{
			name:    "the author's own new document sends nothing",
			creates: true,
		},
		{
			name:     "an archived author is not told",
			archived: true,
			byPerson: true,
		},
		{
			name:     "an author holding a full mailbox is not told yet and the write still lands",
			full:     true,
			byPerson: true,
		},
		{
			name:     "a person's new document for the author sends one mail",
			byPerson: true,
			creates:  true,
			wantMail: "a person wrote a new document for you",
		},
		{
			name:     "a person's edit of their new document before the author reads still calls it new",
			byPerson: true,
			creates:  true,
			then: func(t *testing.T, _ string, written *documentstore.Document) *documentstore.Document {
				return personEdits(t, service, written)
			},
			wantMail: "a person wrote a new document for you",
		},
		{
			name:     "a person's edit of their new document after the author read it is an edit",
			byPerson: true,
			creates:  true,
			then: func(t *testing.T, author string, written *documentstore.Document) *documentstore.Document {
				_, err := stores.Mail.Claim(t.Context(), author, 10)
				require.NoError(t, err)

				return personEdits(t, service, written)
			},
			wantMail: "writing version 2.",
		},
		{
			name:     "a person's edit sends one mail naming the version they wrote",
			byPerson: true,
			wantMail: "writing version 2.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			author := stores.SeedAgent(t, agentstore.Agent{Name: uuid.NewString()})
			if tt.archived {
				_, err := stores.Agents.Archive(ctx, []string{author.ID})
				require.NoError(t, err)
			}
			if tt.full {
				for range 100 {
					_, err := stores.Mail.Send(ctx, mailstore.SendParams{
						FromName:  "person",
						ToAgentID: author.ID,
						Body:      "filler",
						Deadline:  time.Hour,
					})
					require.NoError(t, err)
				}
			}

			params := documents.WriteDocumentParams{
				Name: "Documents",
				Type: "RFC",
				Body: uuid.NewString(),
			}
			if !tt.creates {
				draft, err := service.WriteDocument(ctx, documents.WriteDocumentParams{
					AgentID: author.ID,
					Name:    params.Name,
					Type:    params.Type,
					Body:    "draft",
				})
				require.NoError(t, err)
				params.ID = xptr.Ptr(draft.ID)
				params.Version = xptr.Ptr(draft.Version)
			}
			switch {
			case !tt.byPerson:
				params.AgentID = author.ID
			case tt.creates:
				params.For = author.ID
			}
			written, err := service.WriteDocument(ctx, params)
			require.NoError(t, err)
			if tt.then != nil {
				written = tt.then(t, author.ID, written)
			}

			held, err := stores.Mail.ListPage(ctx, author.ID, 200, nil)
			require.NoError(t, err)
			var waiting []mailstore.Mail
			for _, m := range held {
				if m.Kind != mailstore.KindPlain && m.ClaimedAt == nil {
					waiting = append(waiting, m)
				}
			}
			if tt.full {
				errs := queue.TakeErrors()
				require.Len(t, errs, 1)
				require.ErrorIs(t, errs[0], mail.ErrMailboxFull)
			}
			if tt.wantMail == "" {
				assert.Empty(t, waiting)

				return
			}
			require.Len(t, waiting, 1)
			assert.Equal(t, mailstore.KindDocumentEdited, waiting[0].Kind)
			assert.Equal(t, mail.KomradSenderName, waiting[0].FromName)
			require.NotNil(t, waiting[0].SubjectID)
			assert.Equal(t, written.ID, *waiting[0].SubjectID)
			assert.Contains(t, waiting[0].Body, tt.wantMail)
			assert.Equal(t, []string{notices.DocumentWrittenByPerson}, waiting[0].Notices)
		})
	}
}

func newService(stores *storetest.Stores, events *eventstest.Queue) *documents.DocumentService {
	return documents.NewDocumentService(documents.NewDocumentServiceParams{
		Agents:    stores.Agents,
		Documents: stores.Documents,
		Types:     stores.DocumentTypes,
		Events:    events,
	})
}

func personEdits(t *testing.T, service *documents.DocumentService, written *documentstore.Document) *documentstore.Document {
	t.Helper()

	edited, err := service.WriteDocument(t.Context(), documents.WriteDocumentParams{
		ID:      xptr.Ptr(written.ID),
		Version: xptr.Ptr(written.Version),
		Name:    written.Name,
		Type:    written.Type,
		Body:    uuid.NewString(),
	})
	require.NoError(t, err)

	return edited
}
