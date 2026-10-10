package ledger

import (
	"context"

	"komrad/internal/store"
	"komrad/internal/store/notestore"
)

type Mailer struct {
	db store.DBTX
}

func (m *Mailer) InTx(db store.DBTX) *Mailer {
	return &Mailer{db: db}
}

func (m *Mailer) Send(_ context.Context) error {
	return nil
}

type Service struct {
	pool  *store.Pool
	db    store.DBTX
	notes *notestore.NoteStore
	mail  *Mailer
}

func (s *Service) Read(ctx context.Context) error {
	_, err := s.notes.Get(ctx, "a")

	return err
}

func (s *Service) Write(ctx context.Context) error {
	return s.pool.InTx(ctx, func(db store.DBTX) error {
		notes := notestore.NewNoteStore(db)
		if _, err := notes.Get(ctx, "a"); err != nil {
			return err
		}

		if _, err := notestore.NewNoteStore(db).Get(ctx, "b"); err != nil {
			return err
		}

		return s.mail.InTx(db).Send(ctx)
	})
}

func (s *Service) Leak(ctx context.Context) error {
	captured := notestore.NewNoteStore(s.db)

	return s.pool.InTx(ctx, func(db store.DBTX) error {
		if _, err := s.notes.Get(ctx, "a"); err != nil { // want `s.notes.Get runs outside the transaction this function holds`
			return err
		}

		if _, err := captured.Get(ctx, "b"); err != nil { // want `captured.Get runs outside the transaction`
			return err
		}

		if _, err := notestore.NewNoteStore(s.db).Get(ctx, "c"); err != nil { // want `notestore.NewNoteStore is handed a transaction this function was not handed`
			return err
		}

		if _, err := notestore.NewNoteStore(s.pool.GetDB()).Get(ctx, "d"); err != nil { // want `notestore.NewNoteStore is handed a transaction this function was not handed`
			return err
		}

		if err := s.mail.InTx(s.db).Send(ctx); err != nil { // want `s.mail.InTx is handed a transaction this function was not handed`
			return err
		}

		return s.mail.Send(ctx) // want `s.mail.Send runs outside the transaction`
	})
}

func (s *Service) Handle(ctx context.Context, tx store.DBTX) error {
	if _, err := notestore.NewNoteStore(tx).Get(ctx, "a"); err != nil {
		return err
	}

	_, err := s.notes.Get(ctx, "b") // want `s.notes.Get runs outside the transaction`

	return err
}

func (s *Service) Nested(ctx context.Context) error {
	return s.pool.InTx(ctx, func(db store.DBTX) error {
		return s.pool.InTx(ctx, func(inner store.DBTX) error { // want `s.pool.InTx runs outside the transaction`
			_, err := notestore.NewNoteStore(inner).Get(ctx, "a")

			return err
		})
	})
}

func (m *Mailer) GetFrom() string {
	return ""
}

func (s *Service) Getters(ctx context.Context) error {
	return s.pool.InTx(ctx, func(db store.DBTX) error {
		if s.pool.GetEngine() == s.mail.GetFrom() {
			return nil
		}

		_, err := notestore.NewNoteStore(db).Get(ctx, "a")

		return err
	})
}
