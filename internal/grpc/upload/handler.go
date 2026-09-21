package upload

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	uploadv1 "devclub.com/upload/gen/proto/upload/v1"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	// minPartSize is 5MB, the minimum allowed size for S3 multipart upload parts (except the last part).
	minPartSize = 5 * 1024 * 1024
	// downloadChunkSize is 64KB for efficient streaming over gRPC.
	downloadChunkSize = 64 * 1024
)

func normalizeKey(path string) string {
	return strings.TrimPrefix(strings.TrimSpace(path), "/")
}

// Upload handles client-streaming uploads with S3 multipart support, pause/resume, and SHA-256 verification.
func (s *Server) Upload(stream uploadv1.FileService_UploadServer) error {
	ctx := stream.Context()

	// 1. Receive the initial UploadStart message
	firstReq, err := stream.Recv()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return status.Error(codes.InvalidArgument, "upload stream ended before start message")
		}
		return status.Errorf(codes.Internal, "failed to receive start message: %v", err)
	}

	startPayload, ok := firstReq.Payload.(*uploadv1.UploadRequest_Start)
	if !ok || startPayload.Start == nil {
		return status.Error(codes.InvalidArgument, "first message must be an upload start request")
	}

	start := startPayload.Start
	key := normalizeKey(start.Path)
	if key == "" {
		return status.Error(codes.InvalidArgument, "path cannot be empty")
	}

	var (
		uploadID           = start.UploadId
		completedParts     []types.CompletedPart
		nextPartNumber     int32 = 1
		totalUploadedBytes uint64
		hasher             = sha256.New()
	)

	// 2. Initialize or Resume Multipart Upload
	if uploadID != "" {
		// Resume existing upload: list already uploaded parts
		listResp, err := s.s3Client.S3.ListParts(ctx, &s3.ListPartsInput{
			Bucket:   &s.s3Client.Bucket,
			Key:      &key,
			UploadId: &uploadID,
		})
		if err != nil {
			return status.Errorf(codes.NotFound, "failed to resume upload %s: %v", uploadID, err)
		}

		for _, p := range listResp.Parts {
			completedParts = append(completedParts, types.CompletedPart{
				PartNumber: p.PartNumber,
				ETag:       p.ETag,
			})
			if p.PartNumber != nil && *p.PartNumber >= nextPartNumber {
				nextPartNumber = *p.PartNumber + 1
			}
			if p.Size != nil {
				totalUploadedBytes += uint64(*p.Size)
			}
		}
	} else {
		// Start a new multipart upload
		metadata := make(map[string]string)
		if len(start.Sha256) > 0 {
			metadata["sha256"] = hex.EncodeToString(start.Sha256)
		}
		if start.Size > 0 {
			metadata["size"] = fmt.Sprintf("%d", start.Size)
		}

		createInput := &s3.CreateMultipartUploadInput{
			Bucket:   &s.s3Client.Bucket,
			Key:      &key,
			Metadata: metadata,
		}
		if start.ContentType != "" {
			createInput.ContentType = aws.String(start.ContentType)
		}

		createResp, err := s.s3Client.S3.CreateMultipartUpload(ctx, createInput)
		if err != nil {
			return status.Errorf(codes.Internal, "failed to initialize upload: %v", err)
		}
		uploadID = aws.ToString(createResp.UploadId)
	}

	// 3. Receive and buffer chunks
	buffer := make([]byte, 0, minPartSize)

	for {
		req, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			// On client disconnection or stream error, do not abort upload to allow resuming later
			return status.Errorf(codes.Canceled, "stream error during upload: %v", err)
		}

		chunkPayload, ok := req.Payload.(*uploadv1.UploadRequest_Chunk)
		if !ok || chunkPayload.Chunk == nil {
			return status.Error(codes.InvalidArgument, "expected upload chunk payload")
		}

		chunkData := chunkPayload.Chunk.Data
		if len(chunkData) == 0 {
			continue
		}

		hasher.Write(chunkData)
		buffer = append(buffer, chunkData...)
		totalUploadedBytes += uint64(len(chunkData))

		// When buffer reaches minPartSize (5MB), upload a part
		if len(buffer) >= minPartSize {
			partResp, err := s.s3Client.S3.UploadPart(ctx, &s3.UploadPartInput{
				Bucket:        &s.s3Client.Bucket,
				Key:           &key,
				UploadId:      &uploadID,
				PartNumber:    aws.Int32(nextPartNumber),
				Body:          bytes.NewReader(buffer),
				ContentLength: aws.Int64(int64(len(buffer))),
			})
			if err != nil {
				return status.Errorf(codes.Internal, "failed to upload part %d: %v", nextPartNumber, err)
			}

			completedParts = append(completedParts, types.CompletedPart{
				PartNumber: aws.Int32(nextPartNumber),
				ETag:       partResp.ETag,
			})
			nextPartNumber++
			buffer = buffer[:0]
		}
	}

	// 4. Upload remaining buffer as the final part (if any data left or 0-byte upload)
	if len(buffer) > 0 || len(completedParts) == 0 {
		partResp, err := s.s3Client.S3.UploadPart(ctx, &s3.UploadPartInput{
			Bucket:        &s.s3Client.Bucket,
			Key:           &key,
			UploadId:      &uploadID,
			PartNumber:    aws.Int32(nextPartNumber),
			Body:          bytes.NewReader(buffer),
			ContentLength: aws.Int64(int64(len(buffer))),
		})
		if err != nil {
			return status.Errorf(codes.Internal, "failed to upload final part: %v", err)
		}

		completedParts = append(completedParts, types.CompletedPart{
			PartNumber: aws.Int32(nextPartNumber),
			ETag:       partResp.ETag,
		})
	}

	// 5. Complete Multipart Upload
	sort.Slice(completedParts, func(i, j int) bool {
		return aws.ToInt32(completedParts[i].PartNumber) < aws.ToInt32(completedParts[j].PartNumber)
	})

	_, err = s.s3Client.S3.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
		Bucket:   &s.s3Client.Bucket,
		Key:      &key,
		UploadId: &uploadID,
		MultipartUpload: &types.CompletedMultipartUpload{
			Parts: completedParts,
		},
	})
	if err != nil {
		return status.Errorf(codes.Internal, "failed to complete multipart upload: %v", err)
	}

	computedSha256 := hasher.Sum(nil)
	if len(start.Sha256) > 0 && start.Offset == 0 && !bytes.Equal(start.Sha256, computedSha256) {
		return status.Error(codes.DataLoss, "computed SHA-256 does not match provided checksum")
	}

	return stream.SendAndClose(&uploadv1.UploadResponse{
		UploadId: uploadID,
		Path:     start.Path,
		Size:     totalUploadedBytes,
		Sha256:   computedSha256,
	})
}

// Stat retrieves object metadata (size, content-type, checksum, timestamps).
func (s *Server) Stat(ctx context.Context, req *uploadv1.StatRequest) (*uploadv1.StatResponse, error) {
	if req == nil || strings.TrimSpace(req.Path) == "" {
		return nil, status.Error(codes.InvalidArgument, "path is required")
	}

	key := normalizeKey(req.Path)
	headResp, err := s.s3Client.S3.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: &s.s3Client.Bucket,
		Key:    &key,
	})
	if err != nil {
		var notFound *types.NotFound
		var noSuchKey *types.NoSuchKey
		if errors.As(err, &notFound) || errors.As(err, &noSuchKey) || strings.Contains(err.Error(), "404") || strings.Contains(err.Error(), "NotFound") {
			return nil, status.Error(codes.NotFound, "file not found")
		}
		return nil, status.Errorf(codes.Internal, "failed to stat object: %v", err)
	}

	var (
		size           = uint64(aws.ToInt64(headResp.ContentLength))
		contentType    = aws.ToString(headResp.ContentType)
		modifiedAtUnix int64
		createdAtUnix  int64
		sha256Bytes    []byte
	)

	if headResp.LastModified != nil {
		modifiedAtUnix = headResp.LastModified.Unix()
		createdAtUnix = modifiedAtUnix
	}

	if headResp.Metadata != nil {
		if val, ok := headResp.Metadata["sha256"]; ok {
			sha256Bytes, _ = hex.DecodeString(val)
		}
	}

	return &uploadv1.StatResponse{
		Path:           req.Path,
		Size:           size,
		ContentType:    contentType,
		Sha256:         sha256Bytes,
		CreatedAtUnix:  createdAtUnix,
		ModifiedAtUnix: modifiedAtUnix,
	}, nil
}

// Download streams object content in chunks with optional offset and length range support.
func (s *Server) Download(req *uploadv1.DownloadRequest, stream uploadv1.FileService_DownloadServer) error {
	if req == nil || strings.TrimSpace(req.Path) == "" {
		return status.Error(codes.InvalidArgument, "path is required")
	}

	key := normalizeKey(req.Path)
	getInput := &s3.GetObjectInput{
		Bucket: &s.s3Client.Bucket,
		Key:    &key,
	}

	if req.Offset > 0 || req.Length > 0 {
		if req.Length > 0 {
			getInput.Range = aws.String(fmt.Sprintf("bytes=%d-%d", req.Offset, req.Offset+req.Length-1))
		} else {
			getInput.Range = aws.String(fmt.Sprintf("bytes=%d-", req.Offset))
		}
	}

	getResp, err := s.s3Client.S3.GetObject(stream.Context(), getInput)
	if err != nil {
		var notFound *types.NotFound
		var noSuchKey *types.NoSuchKey
		if errors.As(err, &notFound) || errors.As(err, &noSuchKey) || strings.Contains(err.Error(), "404") || strings.Contains(err.Error(), "NoSuchKey") {
			return status.Error(codes.NotFound, "file not found")
		}
		return status.Errorf(codes.Internal, "failed to get object: %v", err)
	}
	defer getResp.Body.Close()

	buf := make([]byte, downloadChunkSize)
	for {
		n, readErr := getResp.Body.Read(buf)
		if n > 0 {
			if sendErr := stream.Send(&uploadv1.DownloadResponse{
				Data: buf[:n],
			}); sendErr != nil {
				return sendErr
			}
		}

		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return status.Errorf(codes.Internal, "error reading object stream: %v", readErr)
		}
	}

	return nil
}

// Delete removes an object from storage.
func (s *Server) Delete(ctx context.Context, req *uploadv1.DeleteRequest) (*uploadv1.DeleteResponse, error) {
	if req == nil || strings.TrimSpace(req.Path) == "" {
		return nil, status.Error(codes.InvalidArgument, "path is required")
	}

	key := normalizeKey(req.Path)
	_, err := s.s3Client.S3.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: &s.s3Client.Bucket,
		Key:    &key,
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to delete object: %v", err)
	}

	return &uploadv1.DeleteResponse{}, nil
}
