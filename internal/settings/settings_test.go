package settings

import (
	"context"
	"path/filepath"
	"testing"

	"pier/internal/platform/apperr"
	"pier/internal/platform/i18n"
	"pier/internal/platform/sqlite"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	db, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return NewStore(db)
}

func TestAnInstanceWithoutAChoiceSpeaksPortuguese(t *testing.T) {
	current, err := newStore(t).Get(context.Background())
	if err != nil || current.Language != i18n.Portuguese {
		t.Fatalf("got %+v, %v", current, err)
	}
}

func TestTheLanguageIsSavedAndBecomesTheInstances(t *testing.T) {
	defer i18n.SetInstance(i18n.Instance())
	ctx := context.Background()
	store := newStore(t)
	if _, err := store.Update(ctx, Settings{InstanceName: "Pier", Language: "en-US"}); err != nil {
		t.Fatal(err)
	}
	if i18n.Instance() != i18n.English {
		t.Errorf("instance language is %s", i18n.Instance())
	}
	// Clients that predate the field send no language: the choice stays.
	saved, err := store.Update(ctx, Settings{InstanceName: "Pier"})
	if err != nil || saved.Language != i18n.English {
		t.Errorf("got %+v, %v", saved, err)
	}
	i18n.SetInstance(i18n.Portuguese)
	if err := store.LoadLanguage(ctx); err != nil || i18n.Instance() != i18n.English {
		t.Errorf("loading at startup gave %s, %v", i18n.Instance(), err)
	}
}

func TestOnlySupportedLanguagesAreAccepted(t *testing.T) {
	_, err := newStore(t).Update(context.Background(), Settings{InstanceName: "Pier", Language: "fr"})
	appErr, ok := apperr.As(err)
	if !ok || appErr.Fields["language"] == "" {
		t.Errorf("expected a field error on language, got %v", err)
	}
}
