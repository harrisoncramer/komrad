package store_test

import (
	"testing"

	"komrad/internal/dbtest"
	"komrad/internal/store"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const issuer = "https://accounts.example.com"

func grantAccess(t *testing.T, ownerURL, engine, email string) {
	t.Helper()

	require.NoError(t, store.GrantAccess(t.Context(), store.GrantAccessParams{
		OwnerURL: ownerURL,
		Engine:   engine,
		Email:    email,
	}))
}

func claimAccess(t *testing.T, pool store.DBTX, email, subject string) bool {
	t.Helper()

	claimed, err := store.NewAccessStore(pool).ClaimAccess(t.Context(), store.ClaimAccessParams{
		Email:   email,
		Issuer:  issuer,
		Subject: subject,
	})
	require.NoError(t, err)

	return claimed
}

func isGranted(t *testing.T, pool store.DBTX, subject string) bool {
	t.Helper()

	granted, err := store.NewAccessStore(pool).IsAccessGranted(t.Context(), issuer, subject)
	require.NoError(t, err)

	return granted
}

// TestGrantingAccessRefusesAMissingEngineOrAMalformedEmail pins grant refusing an engine nobody ensured and anything but a bare email address.
func TestGrantingAccessRefusesAMissingEngineOrAMalformedEmail(t *testing.T) {
	t.Parallel()

	database := dbtest.New(t)
	engine := openEngine(t, dbtest.NewEngine(t, database)).GetEngine().Name

	tests := []struct {
		name   string
		engine string
		email  string
		want   error
	}{
		{
			name:   "an engine nobody ensured",
			engine: "absent",
			email:  "harry@example.com",
			want:   store.ErrNotFound,
		},
		{
			name:   "a name beside the email",
			engine: engine,
			email:  "Harry <harry@example.com>",
			want:   store.ErrNotAnEmail,
		},
		{
			name:   "no address at all",
			engine: engine,
			email:  "harry",
			want:   store.ErrNotAnEmail,
		},
		{
			name:   "space around the email",
			engine: engine,
			email:  " harry@example.com",
			want:   store.ErrNotAnEmail,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := store.GrantAccess(t.Context(), store.GrantAccessParams{
				OwnerURL: database.OwnerURL,
				Engine:   tt.engine,
				Email:    tt.email,
			})

			require.ErrorIs(t, err, tt.want)
		})
	}
}

// TestRevokingAccessTurnsTheAccountAway pins revoke refusing an email with no grant and removing the grant and the account its sign-in recorded.
func TestRevokingAccessTurnsTheAccountAway(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	database := dbtest.New(t)
	engine := openEngine(t, dbtest.NewEngine(t, database))
	revoke := store.RevokeAccessParams{
		OwnerURL: database.OwnerURL,
		Engine:   engine.GetEngine().Name,
		Email:    "Harry@Example.com",
	}

	require.ErrorIs(t, store.RevokeAccess(ctx, revoke), store.ErrNotFound, "nothing was granted to revoke")

	grantAccess(t, database.OwnerURL, engine.GetEngine().Name, "Harry@Example.com")
	grantAccess(t, database.OwnerURL, engine.GetEngine().Name, "harry@example.com")
	require.True(t, claimAccess(t, engine.GetDB(), "harry@example.com", "first"), "granting twice in any case leaves one grant to claim")

	require.NoError(t, store.RevokeAccess(ctx, revoke))
	assert.False(t, isGranted(t, engine.GetDB(), "first"))
}

// TestTheFirstSignInClaimsAnEmailForGood pins a granted email taking the first account to claim it and no other.
func TestTheFirstSignInClaimsAnEmailForGood(t *testing.T) {
	t.Parallel()

	database := dbtest.New(t)
	engine := openEngine(t, dbtest.NewEngine(t, database))
	pool := engine.GetDB()
	grantAccess(t, database.OwnerURL, engine.GetEngine().Name, "harry@example.com")
	grantAccess(t, database.OwnerURL, engine.GetEngine().Name, "harry@work.example.com")

	assert.False(t, isGranted(t, pool, "first"), "nobody has signed in yet")
	assert.False(t, claimAccess(t, pool, "sally@example.com", "first"), "an email nobody granted claims nothing")

	require.True(t, claimAccess(t, pool, "Harry@Example.com", "first"), "the email matches whatever its case")
	assert.True(t, isGranted(t, pool, "first"))

	assert.False(t, claimAccess(t, pool, "harry@example.com", "second"), "a claimed email stays with its first account")
	assert.False(t, isGranted(t, pool, "second"))

	assert.False(t, claimAccess(t, pool, "harry@work.example.com", "first"), "an account holds one grant on an engine")
	assert.False(t, claimAccess(t, pool, "harry@work.example.com", ""), "a claim needs an account")
}

// TestAccessBelongsToTheEngineItWasGrantedOn pins one engine's grants being invisible to every other.
func TestAccessBelongsToTheEngineItWasGrantedOn(t *testing.T) {
	t.Parallel()

	database := dbtest.New(t)
	first := openEngine(t, dbtest.NewEngine(t, database))
	second := openEngine(t, dbtest.NewEngine(t, database)).GetDB()
	grantAccess(t, database.OwnerURL, first.GetEngine().Name, "harry@example.com")

	assert.False(t, claimAccess(t, second, "harry@example.com", "first"), "another engine's grant claims nothing")
	require.True(t, claimAccess(t, first.GetDB(), "harry@example.com", "first"))
	assert.False(t, isGranted(t, second, "first"), "another engine's grant lets nobody in")
}

// TestTheEngineReachesAccessOnlyThroughItsFunctions pins the engine login refused the table and the reader login refused the table and both functions.
func TestTheEngineReachesAccessOnlyThroughItsFunctions(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	database := dbtest.New(t)
	engine := dbtest.NewEngine(t, database)
	writer := openEngine(t, engine)

	reader, err := store.OpenReadOnly(ctx, engine.ReaderURL, writer.GetEngine().ID)
	require.NoError(t, err)
	t.Cleanup(reader.Close)

	tests := []struct {
		name string
		pool store.DBTX
		sql  string
	}{
		{
			name: "the engine granting",
			pool: writer.GetDB(),
			sql:  "INSERT INTO engine_access (email) VALUES ('harry@example.com')",
		},
		{
			name: "the engine reading",
			pool: writer.GetDB(),
			sql:  "SELECT email FROM engine_access",
		},
		{
			name: "the reader reading",
			pool: reader,
			sql:  "SELECT email FROM engine_access",
		},
		{
			name: "the reader asking",
			pool: reader,
			sql:  "SELECT engine_access_granted('issuer', 'subject')",
		},
		{
			name: "the reader claiming",
			pool: reader,
			sql:  "SELECT claim_engine_access('harry@example.com', 'issuer', 'subject')",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := tt.pool.Exec(ctx, tt.sql)

			var pgErr *pgconn.PgError
			require.ErrorAs(t, err, &pgErr)
			assert.Equal(t, "42501", pgErr.Code, "permission denied")
		})
	}
}
