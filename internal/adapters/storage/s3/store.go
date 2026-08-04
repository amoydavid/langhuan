package s3

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"

	portstorage "github.com/dajee/langhuan/internal/ports/storage"
)

// s3API 是 S3 客户端的最小接口，便于在测试中用 fake 替换。
type s3API interface {
	PutObject(ctx context.Context, input *s3.PutObjectInput, opts ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	GetObject(ctx context.Context, input *s3.GetObjectInput, opts ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	DeleteObject(ctx context.Context, input *s3.DeleteObjectInput, opts ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
}

// Config 描述 S3-compatible 存储的连接参数。
type Config struct {
	Endpoint       string
	Region         string
	Bucket         string
	AccessKey      string
	SecretKey      string
	ForcePathStyle bool
	PublicBaseURL  string
}

// Store 持有共享的 S3 客户端，由 RawDocumentStore 和 AssetStore 两个子类型嵌入。
type Store struct {
	client        s3API
	bucket        string
	publicBaseURL string
}

// NewStore 用 AWS SDK v2 构建 S3 客户端并返回 Store。
func NewStore(ctx context.Context, cfg Config) (*Store, error) {
	if cfg.Bucket == "" {
		return nil, errors.New("S3 bucket 不能为空")
	}
	awsCfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(cfg.Region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, "")),
	)
	if err != nil {
		return nil, fmt.Errorf("加载 S3 配置失败: %w", err)
	}
	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if cfg.Endpoint != "" {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
		}
		o.UsePathStyle = cfg.ForcePathStyle
	})
	return &Store{
		client:        client,
		bucket:        cfg.Bucket,
		publicBaseURL: strings.TrimRight(cfg.PublicBaseURL, "/"),
	}, nil
}

// NewRawDocumentStore 返回实现 portstorage.RawDocumentStore 的 S3 适配器。
func (s *Store) NewRawDocumentStore() *RawDocumentStore {
	return &RawDocumentStore{store: s}
}

// NewAssetStore 返回实现 portstorage.AssetStore 的 S3 适配器。
func (s *Store) NewAssetStore() *AssetStore {
	return &AssetStore{store: s}
}

// newStoreWithClient 供测试使用，注入自定义 s3API。
func newStoreWithClient(client s3API, bucket, publicBaseURL string) *Store {
	return &Store{
		client:        client,
		bucket:        bucket,
		publicBaseURL: strings.TrimRight(publicBaseURL, "/"),
	}
}

// PutObject 上传一段 bytes 到指定 key，供 raw markdown 归档等场景使用。
// 它不是任一 port 接口的一部分，而是 Store 的便捷方法。
func (s *Store) PutObject(ctx context.Context, key, contentType string, data []byte) (*portstorage.StoredObject, error) {
	hash := sha256.Sum256(data)
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(data),
		ContentType: aws.String(contentType),
	})
	if err != nil {
		return nil, fmt.Errorf("上传对象到 S3 失败: %w", err)
	}
	return &portstorage.StoredObject{
		Key:       key,
		PublicURL: s.composePublicURL(key),
		SizeBytes: int64(len(data)),
		SHA256:    hex.EncodeToString(hash[:]),
	}, nil
}

func (s *Store) deleteObject(ctx context.Context, key string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("从 S3 删除对象失败: %w", err)
	}
	return nil
}

func (s *Store) composePublicURL(key string) string {
	if s.publicBaseURL == "" {
		return ""
	}
	parts := strings.Split(key, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return s.publicBaseURL + "/" + strings.Join(parts, "/")
}

// ---- RawDocumentStore ----

// RawDocumentStore 实现 portstorage.RawDocumentStore。
type RawDocumentStore struct {
	store *Store
}

func (r *RawDocumentStore) Put(ctx context.Context, input portstorage.RawDocumentInput) (*portstorage.RawDocumentObject, error) {
	if input.Reader == nil {
		return nil, errors.New("raw document reader is nil")
	}
	key := RawDocumentKey(input.WorkspaceID, input.KnowledgeBaseID, input.DocumentID, uuid.Nil, input.FileName)
	hash := sha256.New()
	tee := io.TeeReader(input.Reader, hash)
	_, err := r.store.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(r.store.bucket),
		Key:         aws.String(key),
		Body:        tee,
		ContentType: aws.String(input.ContentType),
	})
	if err != nil {
		return nil, fmt.Errorf("上传原始文件到 S3 失败: %w", err)
	}
	return &portstorage.RawDocumentObject{
		Key:         key,
		SizeBytes:   input.SizeBytes,
		SHA256:      hex.EncodeToString(hash.Sum(nil)),
		ContentType: input.ContentType,
	}, nil
}

func (r *RawDocumentStore) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	out, err := r.store.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(r.store.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("从 S3 读取对象失败: %w", err)
	}
	return out.Body, nil
}

func (r *RawDocumentStore) Delete(ctx context.Context, key string) error {
	return r.store.deleteObject(ctx, key)
}

// ---- AssetStore ----

// AssetStore 实现 portstorage.AssetStore。
type AssetStore struct {
	store *Store
}

func (a *AssetStore) Put(ctx context.Context, object portstorage.ObjectInput) (*portstorage.StoredObject, error) {
	if object.Key == "" {
		return nil, errors.New("asset key is empty")
	}
	hash := sha256.Sum256(object.Data)
	_, err := a.store.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(a.store.bucket),
		Key:         aws.String(object.Key),
		Body:        bytes.NewReader(object.Data),
		ContentType: aws.String(object.MimeType),
	})
	if err != nil {
		return nil, fmt.Errorf("上传资产到 S3 失败: %w", err)
	}
	return &portstorage.StoredObject{
		Key:       object.Key,
		PublicURL: a.store.composePublicURL(object.Key),
		SizeBytes: int64(len(object.Data)),
		SHA256:    hex.EncodeToString(hash[:]),
	}, nil
}

func (a *AssetStore) Delete(ctx context.Context, key string) error {
	return a.store.deleteObject(ctx, key)
}

// Open 按 storage key 打开已归档的资产内容，供鉴权代理 handler 读取。
func (a *AssetStore) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	out, err := a.store.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(a.store.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("从 S3 读取资产失败: %w", err)
	}
	return out.Body, nil
}

// 编译期确保两个子类型实现各自的 port 接口。
var (
	_ portstorage.RawDocumentStore = (*RawDocumentStore)(nil)
	_ portstorage.AssetStore       = (*AssetStore)(nil)
)
