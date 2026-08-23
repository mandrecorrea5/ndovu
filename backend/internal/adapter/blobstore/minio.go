// Package blobstore implementa SnapshotBlobStore sobre S3/MinIO.
// Interface no domain permite trocar por disco local ou outro provider sem
// tocar no resto.
package blobstore

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/marcoscorrea/ndovu/backend/internal/domain"
)

// Config para inicializar o cliente. Endpoint sem esquema (só host:port).
type Config struct {
	Endpoint  string
	AccessKey string
	SecretKey string
	Bucket    string
	UseSSL    bool
}

// MinIO implementa domain.SnapshotBlobStore.
type MinIO struct {
	client *minio.Client
	bucket string
}

// New conecta e valida o bucket. Retorna erro se o bucket não existir —
// esperamos que o `minio-init` do compose já tenha criado.
func New(ctx context.Context, cfg Config) (*MinIO, error) {
	client, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("minio client: %w", err)
	}
	ok, err := client.BucketExists(ctx, cfg.Bucket)
	if err != nil {
		return nil, fmt.Errorf("verificando bucket %s: %w", cfg.Bucket, err)
	}
	if !ok {
		return nil, fmt.Errorf("bucket %s não existe — cria via minio-init", cfg.Bucket)
	}
	return &MinIO{client: client, bucket: cfg.Bucket}, nil
}

// PutSnapshot grava. Content-Type application/gzip porque o serviço grava
// HTML compactado (SDK/backend fazem gzip antes).
func (m *MinIO) PutSnapshot(ctx context.Context, key string, content []byte) error {
	_, err := m.client.PutObject(ctx, m.bucket, key,
		bytes.NewReader(content), int64(len(content)),
		minio.PutObjectOptions{ContentType: "application/gzip"})
	if err != nil {
		return fmt.Errorf("putting snapshot %s: %w", key, err)
	}
	return nil
}

// GetSnapshot lê tudo. Snapshots são pequenos (~50KB gzip); o consumer
// descomprime antes de servir ao browser.
func (m *MinIO) GetSnapshot(ctx context.Context, key string) ([]byte, error) {
	obj, err := m.client.GetObject(ctx, m.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("get %s: %w", key, err)
	}
	defer obj.Close()
	data, err := io.ReadAll(obj)
	if err != nil {
		// MinIO devolve NoSuchKey aqui, não no GetObject inicial.
		if isNotFound(err) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("lendo %s: %w", key, err)
	}
	return data, nil
}

func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	if minioErr, ok := err.(minio.ErrorResponse); ok {
		return minioErr.Code == "NoSuchKey"
	}
	return false
}
