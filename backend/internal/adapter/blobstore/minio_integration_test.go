//go:build integration

package blobstore_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/marcoscorrea/ndovu/backend/internal/adapter/testenv"
	"github.com/marcoscorrea/ndovu/backend/internal/domain"
)

func TestBlobstore_PutGetRoundTrip(t *testing.T) {
	store := testenv.StartMinIO(t)
	ctx := context.Background()

	payload := []byte("<html><body>hello world</body></html>")
	if err := store.PutSnapshot(ctx, "test/e-1.html.gz", payload); err != nil {
		t.Fatalf("put: %v", err)
	}

	got, err := store.GetSnapshot(ctx, "test/e-1.html.gz")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Errorf("payload divergente: %d vs %d bytes", len(got), len(payload))
	}
}

func TestBlobstore_GetKeyInexistente(t *testing.T) {
	store := testenv.StartMinIO(t)
	_, err := store.GetSnapshot(context.Background(), "nao/existe.html.gz")
	if err != domain.ErrNotFound {
		t.Errorf("esperava ErrNotFound, veio %v", err)
	}
}

func TestBlobstore_SnapshotGrande(t *testing.T) {
	// Snapshot típico após gzip fica em ~5-50KB. Testamos 200KB pra ter
	// margem confortável de que payloads reais passam.
	store := testenv.StartMinIO(t)
	ctx := context.Background()
	payload := []byte(strings.Repeat("x", 200*1024))

	if err := store.PutSnapshot(ctx, "big/snapshot.gz", payload); err != nil {
		t.Fatalf("put grande: %v", err)
	}
	got, err := store.GetSnapshot(ctx, "big/snapshot.gz")
	if err != nil {
		t.Fatalf("get grande: %v", err)
	}
	if len(got) != len(payload) {
		t.Errorf("tamanho divergente: %d vs %d", len(got), len(payload))
	}
}

func TestBlobstore_PutSobrescreveMesmaChave(t *testing.T) {
	// S3 é "last write wins" — segundo put na mesma chave sobrescreve.
	store := testenv.StartMinIO(t)
	ctx := context.Background()
	key := "same/key.gz"

	if err := store.PutSnapshot(ctx, key, []byte("versao-1")); err != nil {
		t.Fatalf("put 1: %v", err)
	}
	if err := store.PutSnapshot(ctx, key, []byte("versao-2")); err != nil {
		t.Fatalf("put 2: %v", err)
	}
	got, err := store.GetSnapshot(ctx, key)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if string(got) != "versao-2" {
		t.Errorf("esperava versao-2, veio %q", string(got))
	}
}
