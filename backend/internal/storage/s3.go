package storage

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type S3Client struct {
	client     *s3.Client
	bucketName string
}

type S3Config struct {
	Region          string
	AccessKeyID     string
	SecretAccessKey string
	EndpointURL     string // Used for local MinIO
	BucketName      string
}

// NewS3Client creates a new configured S3 client.
// bucket: S3 bucket name; region: AWS region; endpoint: optional custom endpoint (e.g. MinIO).
func NewS3Client(ctx context.Context, bucket, region, endpoint string) (*S3Client, error) {
	customResolver := aws.EndpointResolverWithOptionsFunc(func(service, reg string, options ...interface{}) (aws.Endpoint, error) {
		if endpoint != "" {
			return aws.Endpoint{
				PartitionID:   "aws",
				URL:           endpoint,
				SigningRegion: region,
			}, nil
		}
		return aws.Endpoint{}, &aws.EndpointNotFoundError{}
	})

	awsCfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(region),
		config.WithEndpointResolverWithOptions(customResolver),
	)
	if err != nil {
		return nil, fmt.Errorf("unable to load SDK config: %w", err)
	}

	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if endpoint != "" {
			o.UsePathStyle = true
		}
	})

	return &S3Client{
		client:     client,
		bucketName: bucket,
	}, nil
}

// UploadFile uploads an io.Reader to the specified S3 object key
func (s *S3Client) UploadFile(ctx context.Context, objectKey string, body io.Reader) error {
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.bucketName),
		Key:    aws.String(objectKey),
		Body:   body,
	})
	if err != nil {
		return fmt.Errorf("failed to upload object: %w", err)
	}
	return nil
}

// DownloadFile downloads an S3 object and writes it to dst.
func (s *S3Client) DownloadFile(ctx context.Context, objectKey string, dst io.Writer) error {
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucketName),
		Key:    aws.String(objectKey),
	})
	if err != nil {
		return fmt.Errorf("failed to get object %s: %w", objectKey, err)
	}
	defer out.Body.Close()
	_, err = io.Copy(dst, out.Body)
	return err
}

// GeneratePresignedUploadURL generates a URL that the frontend can use to upload a file directly to S3.
func (s *S3Client) GeneratePresignedUploadURL(ctx context.Context, objectKey string, expiry time.Duration) (string, error) {
	presignClient := s3.NewPresignClient(s.client)

	req, err := presignClient.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.bucketName),
		Key:    aws.String(objectKey),
	}, func(opts *s3.PresignOptions) {
		opts.Expires = expiry
	})
	if err != nil {
		return "", fmt.Errorf("failed to generate presigned url: %w", err)
	}
	return req.URL, nil
}
