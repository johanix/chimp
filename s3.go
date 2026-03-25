package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"regexp"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// S3File represents a discovered file in S3 with its parsed metadata.
type S3File struct {
	Key       string
	Hostname  string
	Timestamp time.Time
	UUID      string
	Filename  string
}

// pathRegex parses the Hive-style S3 prefix:
// year=YYYY/month=MM/day=DD/hour=HH/minute=MM/second=SS/hostname=HOST/uuid=UUID/FILENAME
var pathRegex = regexp.MustCompile(
	`year=(\d{4})/month=(\d{2})/day=(\d{2})/hour=(\d{2})/minute=(\d{2})/second=(\d{2})/hostname=([^/]+)/uuid=([^/]+)/(.+)$`,
)

func parseS3Key(key string) (*S3File, error) {
	m := pathRegex.FindStringSubmatch(key)
	if m == nil {
		return nil, fmt.Errorf("key does not match expected pattern: %s", key)
	}
	ts, err := time.Parse("2006-01-02T15:04:05",
		fmt.Sprintf("%s-%s-%sT%s:%s:%s", m[1], m[2], m[3], m[4], m[5], m[6]))
	if err != nil {
		return nil, fmt.Errorf("parsing timestamp from key: %w", err)
	}
	return &S3File{
		Key:       key,
		Hostname:  m[7],
		Timestamp: ts,
		UUID:      m[8],
		Filename:  m[9],
	}, nil
}

type S3Client struct {
	client *s3.Client
	bucket string
	prefix string
}

func NewS3Client(ctx context.Context, cfg S3Config) (*S3Client, error) {
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

	return &S3Client{
		client: s3.NewFromConfig(awsCfg, clientOpts),
		bucket: cfg.Bucket,
		prefix: cfg.Prefix,
	}, nil
}

// ListFiles returns all S3 keys under the configured prefix that match
// the expected path pattern. It uses the provided time prefix to narrow
// the listing (e.g., "year=2026/month=03/day=25/").
func (sc *S3Client) ListFiles(ctx context.Context, timePrefix string) ([]S3File, error) {
	prefix := sc.prefix
	if timePrefix != "" {
		if prefix == "" {
			prefix = timePrefix
		} else {
			prefix = strings.TrimRight(prefix, "/") + "/" + timePrefix
		}
	}

	var files []S3File
	paginator := s3.NewListObjectsV2Paginator(sc.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(sc.bucket),
		Prefix: aws.String(prefix),
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("listing S3 objects: %w", err)
		}
		for _, obj := range page.Contents {
			sf, err := parseS3Key(*obj.Key)
			if err != nil {
				log.Printf("skipping unrecognized key: %s", *obj.Key)
				continue
			}
			files = append(files, *sf)
		}
	}
	return files, nil
}

// FetchFile downloads a file from S3 and returns its contents.
func (sc *S3Client) FetchFile(ctx context.Context, key string) ([]byte, error) {
	resp, err := sc.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(sc.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", key, err)
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}
