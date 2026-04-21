// Package s3io provides shared S3 primitives for DSC pipeline binaries:
// client construction, Hive-style path parsing, and basic list/fetch/put.
package s3io

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"regexp"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
)

type Config struct {
	Bucket   string `yaml:"bucket"`
	Prefix   string `yaml:"prefix"`
	Region   string `yaml:"region"`
	Endpoint string `yaml:"endpoint"`
	Profile  string `yaml:"profile"`
}

// File represents a DSC file in S3 with its parsed Hive-path metadata.
type File struct {
	Key       string
	Provider  string
	Site      string
	Hostname  string
	Timestamp time.Time
	UUID      string
	Filename  string
}

// pathRegex parses the Hive-style S3 key:
// year=YYYY/month=MM/day=DD/hour=HH/minute=MM/second=SS/provider=PROV/site=SITE/hostname=HOST/uuid=UUID/FILENAME
var pathRegex = regexp.MustCompile(
	`year=(\d{4})/month=(\d{2})/day=(\d{2})/hour=(\d{2})/minute=(\d{2})/second=(\d{2})/provider=([^/]+)/site=([^/]+)/hostname=([^/]+)/uuid=([^/]+)/(.+)$`,
)

// ParseKey extracts DSC file metadata from a Hive-style S3 key.
func ParseKey(key string) (*File, error) {
	m := pathRegex.FindStringSubmatch(key)
	if m == nil {
		return nil, fmt.Errorf("key does not match expected pattern: %s", key)
	}
	ts, err := time.Parse("2006-01-02T15:04:05",
		fmt.Sprintf("%s-%s-%sT%s:%s:%s", m[1], m[2], m[3], m[4], m[5], m[6]))
	if err != nil {
		return nil, fmt.Errorf("parsing timestamp from key: %w", err)
	}
	return &File{
		Key:       key,
		Provider:  m[7],
		Site:      m[8],
		Hostname:  m[9],
		Timestamp: ts,
		UUID:      m[10],
		Filename:  m[11],
	}, nil
}

// BuildKey constructs a Hive-style S3 key for a DSC file.
// prefix may be empty; t is assumed UTC by callers.
func BuildKey(prefix, provider, site, hostname, uuid, filename string, t time.Time) string {
	ts := t.UTC()
	key := fmt.Sprintf(
		"year=%04d/month=%02d/day=%02d/hour=%02d/minute=%02d/second=%02d/provider=%s/site=%s/hostname=%s/uuid=%s/%s",
		ts.Year(), ts.Month(), ts.Day(), ts.Hour(), ts.Minute(), ts.Second(),
		provider, site, hostname, uuid, filename,
	)
	if prefix != "" {
		key = strings.TrimRight(prefix, "/") + "/" + key
	}
	return key
}

type Client struct {
	client *s3.Client
	bucket string
	prefix string
}

func NewClient(ctx context.Context, cfg Config) (*Client, error) {
	opts := []func(*config.LoadOptions) error{
		config.WithRegion(cfg.Region),
	}
	if cfg.Profile != "" {
		opts = append(opts, config.WithSharedConfigProfile(cfg.Profile))
	}

	awsCfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("loading AWS config: %w", err)
	}

	clientOpts := func(o *s3.Options) {}
	if cfg.Endpoint != "" {
		clientOpts = func(o *s3.Options) {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
			o.UsePathStyle = true
		}
	}

	return &Client{
		client: s3.NewFromConfig(awsCfg, clientOpts),
		bucket: cfg.Bucket,
		prefix: cfg.Prefix,
	}, nil
}

func (c *Client) Bucket() string { return c.bucket }
func (c *Client) Prefix() string { return c.prefix }

// List returns all S3 files under the configured prefix joined with
// timePrefix (e.g. "year=2026/month=03/day=25/"). Keys that do not
// match the Hive layout are skipped with a log message.
func (c *Client) List(ctx context.Context, timePrefix string) ([]File, error) {
	prefix := c.prefix
	if timePrefix != "" {
		if prefix == "" {
			prefix = timePrefix
		} else {
			prefix = strings.TrimRight(prefix, "/") + "/" + timePrefix
		}
	}

	var files []File
	paginator := s3.NewListObjectsV2Paginator(c.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(c.bucket),
		Prefix: aws.String(prefix),
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("listing S3 objects: %w", err)
		}
		for _, obj := range page.Contents {
			f, err := ParseKey(*obj.Key)
			if err != nil {
				log.Printf("skipping unrecognized key: %s", *obj.Key)
				continue
			}
			files = append(files, *f)
		}
	}
	return files, nil
}

// Fetch downloads an object and returns its contents.
func (c *Client) Fetch(ctx context.Context, key string) ([]byte, error) {
	resp, err := c.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", key, err)
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

// Exists returns true if an object with the given key exists in the bucket.
func (c *Client) Exists(ctx context.Context, key string) (bool, error) {
	_, err := c.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	if err == nil {
		return true, nil
	}
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.ErrorCode() {
		case "NotFound", "NoSuchKey":
			return false, nil
		}
	}
	return false, err
}

// Put writes data to the bucket under key with the given Content-Type.
func (c *Client) Put(ctx context.Context, key string, data []byte, contentType string) error {
	_, err := c.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(c.bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(data),
		ContentType: aws.String(contentType),
	})
	if err != nil {
		return fmt.Errorf("putting %s: %w", key, err)
	}
	return nil
}
