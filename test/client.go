package test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"strings"

	uploadv1 "devclub.com/upload/gen/proto/upload/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

// Client is a high-level test client for interacting with the Upload Service.
type Client struct {
	conn       *grpc.ClientConn
	fileClient uploadv1.FileServiceClient
	token      string
}

// NewClient connects to the Upload Service gRPC server and sets the authorization token.
func NewClient(target string, token string, opts ...grpc.DialOption) (*Client, error) {
	defaultOpts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	}
	defaultOpts = append(defaultOpts, opts...)

	conn, err := grpc.NewClient(target, defaultOpts...)
	if err != nil {
		return nil, fmt.Errorf("dial upload service: %w", err)
	}

	return &Client{
		conn:       conn,
		fileClient: uploadv1.NewFileServiceClient(conn),
		token:      strings.TrimSpace(token),
	}, nil
}

// SetToken updates the authorization token for subsequent requests.
func (c *Client) SetToken(token string) {
	c.token = strings.TrimSpace(token)
}

// withAuth attaches the Bearer token to the outgoing context metadata.
func (c *Client) withAuth(ctx context.Context) context.Context {
	if c.token == "" {
		return ctx
	}
	return metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+c.token)
}

// Upload streams a file to the Upload Service in chunks.
func (c *Client) Upload(
	ctx context.Context,
	path string,
	contentType string,
	reader io.Reader,
	size uint64,
	sha256Checksum []byte,
	resumeUploadID string,
	offset uint64,
	chunkSize int,
) (*uploadv1.UploadResponse, error) {
	if chunkSize <= 0 {
		chunkSize = 64 * 1024 // default 64KB chunks
	}

	authCtx := c.withAuth(ctx)
	stream, err := c.fileClient.Upload(authCtx)
	if err != nil {
		return nil, fmt.Errorf("create upload stream: %w", err)
	}

	// 1. Send UploadStart message
	startReq := &uploadv1.UploadRequest{
		Payload: &uploadv1.UploadRequest_Start{
			Start: &uploadv1.UploadStart{
				Path:        path,
				ContentType: contentType,
				Size:        size,
				Sha256:      sha256Checksum,
				UploadId:    resumeUploadID,
				Offset:      offset,
			},
		},
	}
	if err := stream.Send(startReq); err != nil {
		return nil, fmt.Errorf("send upload start: %w", err)
	}

	// 2. Stream data chunks
	buf := make([]byte, chunkSize)
	for {
		n, readErr := reader.Read(buf)
		if n > 0 {
			chunkReq := &uploadv1.UploadRequest{
				Payload: &uploadv1.UploadRequest_Chunk{
					Chunk: &uploadv1.UploadChunk{
						Data: buf[:n],
					},
				},
			}
			if err := stream.Send(chunkReq); err != nil {
				return nil, fmt.Errorf("send upload chunk: %w", err)
			}
		}

		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, fmt.Errorf("read chunk: %w", readErr)
		}
	}

	// 3. Close and receive response
	resp, err := stream.CloseAndRecv()
	if err != nil {
		return nil, fmt.Errorf("close and receive upload response: %w", err)
	}

	return resp, nil
}

// UploadBytes is a convenience method to upload an in-memory byte slice.
func (c *Client) UploadBytes(
	ctx context.Context,
	path string,
	contentType string,
	data []byte,
) (*uploadv1.UploadResponse, error) {
	h := sha256.Sum256(data)
	return c.Upload(
		ctx,
		path,
		contentType,
		bytes.NewReader(data),
		uint64(len(data)),
		h[:],
		"",
		0,
		64*1024,
	)
}

// Stat queries the metadata of an uploaded file.
func (c *Client) Stat(ctx context.Context, path string) (*uploadv1.StatResponse, error) {
	authCtx := c.withAuth(ctx)
	return c.fileClient.Stat(authCtx, &uploadv1.StatRequest{Path: path})
}

// Download downloads file contents, supporting optional offset and length ranges.
func (c *Client) Download(ctx context.Context, path string, offset, length uint64) ([]byte, error) {
	authCtx := c.withAuth(ctx)
	stream, err := c.fileClient.Download(authCtx, &uploadv1.DownloadRequest{
		Path:   path,
		Offset: offset,
		Length: length,
	})
	if err != nil {
		return nil, fmt.Errorf("open download stream: %w", err)
	}

	var out bytes.Buffer
	for {
		resp, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("receive download chunk: %w", err)
		}
		out.Write(resp.Data)
	}

	return out.Bytes(), nil
}

// Delete removes a file from the Upload Service.
func (c *Client) Delete(ctx context.Context, path string) error {
	authCtx := c.withAuth(ctx)
	_, err := c.fileClient.Delete(authCtx, &uploadv1.DeleteRequest{Path: path})
	return err
}

// Close closes the underlying gRPC connection.
func (c *Client) Close() error {
	return c.conn.Close()
}
