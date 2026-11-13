package storage

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// ObjectStore keeps transfer proofs and product photos.
// Local disk is the dev fallback. R2 (S3) is used when configured.
type ObjectStore interface {
	Mode() string
	Put(ctx context.Context, key string, r io.Reader, mime string) error
	Open(ctx context.Context, key string) (io.ReadCloser, error)
	PresignPut(ctx context.Context, key, mime string) (string, error)
}

func SafeKey(key string) (string, error) {
	key = strings.TrimPrefix(filepath.ToSlash(key), "/")
	if key == "" || strings.Contains(key, "..") || strings.Contains(key, "\\") {
		return "", fmt.Errorf("invalid object key")
	}
	for _, part := range strings.Split(key, "/") {
		if part == "" || part == "." || part == ".." {
			return "", fmt.Errorf("invalid object key")
		}
	}
	return key, nil
}

type Local struct {
	Dir string
}

func (l Local) Mode() string { return "local" }

func (l Local) path(key string) (string, error) {
	key, err := SafeKey(key)
	if err != nil {
		return "", err
	}
	return filepath.Join(l.Dir, filepath.FromSlash(key)), nil
}

func (l Local) Put(_ context.Context, key string, r io.Reader, _ string) error {
	p, err := l.path(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	f, err := os.Create(p)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, r)
	return err
}

func (l Local) Open(_ context.Context, key string) (io.ReadCloser, error) {
	p, err := l.path(key)
	if err != nil {
		return nil, err
	}
	return os.Open(p)
}

func (l Local) PresignPut(context.Context, string, string) (string, error) {
	return "", fmt.Errorf("local storage has no presigned upload")
}

type S3 struct {
	Client  *s3.Client
	Presign *s3.PresignClient
	Bucket  string
}

func NewS3(endpoint, bucket, access, secret string) (*S3, error) {
	cfg := aws.Config{
		Region:      "auto",
		Credentials: credentials.NewStaticCredentialsProvider(access, secret, ""),
		EndpointResolverWithOptions: aws.EndpointResolverWithOptionsFunc(func(service, region string, _ ...interface{}) (aws.Endpoint, error) {
			return aws.Endpoint{URL: endpoint, HostnameImmutable: true, SigningRegion: "auto"}, nil
		}),
	}
	client := s3.NewFromConfig(cfg, func(o *s3.Options) { o.UsePathStyle = true })
	return &S3{Client: client, Presign: s3.NewPresignClient(client), Bucket: bucket}, nil
}

func (s *S3) Mode() string { return "s3" }

func (s *S3) Put(ctx context.Context, key string, r io.Reader, mime string) error {
	key, err := SafeKey(key)
	if err != nil {
		return err
	}
	_, err = s.Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.Bucket),
		Key:         aws.String(key),
		Body:        r,
		ContentType: aws.String(mime),
	})
	return err
}

func (s *S3) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	key, err := SafeKey(key)
	if err != nil {
		return nil, err
	}
	out, err := s.Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.Bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, err
	}
	return out.Body, nil
}

func (s *S3) PresignPut(ctx context.Context, key, mime string) (string, error) {
	key, err := SafeKey(key)
	if err != nil {
		return "", err
	}
	req, err := s.Presign.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.Bucket),
		Key:         aws.String(key),
		ContentType: aws.String(mime),
	}, s3.WithPresignExpires(15*time.Minute))
	if err != nil {
		return "", err
	}
	return req.URL, nil
}
