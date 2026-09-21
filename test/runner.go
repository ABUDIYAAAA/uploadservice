package test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"log"
	"os"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestConfig contains the configuration parameters for the test suite.
type TestConfig struct {
	IdentityServiceURL string
	UploadServiceURL   string
	ClientID           string
	ClientSecret       string
	SampleFilePath     string
}

// RunSuite runs a full end-to-end automated test against the Upload Service.
func RunSuite(ctx context.Context, cfg TestConfig) error {
	log.Println("==========================================================")
	log.Println("🚀 Starting Upload Service End-to-End Test Suite")
	log.Println("==========================================================")

	// 1. Obtain token from Identity Service
	log.Printf("[Step 1/7]  Fetching access token from Identity Service (%s)...\n", cfg.IdentityServiceURL)
	idClient := NewIdentityClient(cfg.IdentityServiceURL)
	token, err := idClient.FetchToken(ctx, cfg.ClientID, cfg.ClientSecret)
	if err != nil {
		return fmt.Errorf("failed to fetch token: %w", err)
	}
	log.Printf("  ✔ Successfully acquired access token for client ID: %s\n", cfg.ClientID)

	// 2. Test Unauthenticated / Invalid Token Rejection
	log.Println("[Step 2/7]  Testing authentication interceptor rejection with invalid token...")
	invalidClient, err := NewClient(cfg.UploadServiceURL, "invalid-bearer-token-xyz")
	if err != nil {
		return fmt.Errorf("failed to initialize invalid client: %w", err)
	}
	defer invalidClient.Close()

	_, err = invalidClient.Stat(ctx, "any-path.txt")
	if err == nil {
		return fmt.Errorf("expected unauthenticated error with invalid token, but got success")
	}
	if s, ok := status.FromError(err); ok && s.Code() == codes.Unauthenticated {
		log.Printf("  ✔ Invalid token successfully rejected with codes.Unauthenticated: %v\n", s.Message())
	} else {
		log.Printf("  ✔ Request with invalid token failed as expected: %v\n", err)
	}

	// 3. Connect with Valid Token
	log.Printf("[Step 3/7]  Connecting to Upload Service at %s with valid token...\n", cfg.UploadServiceURL)
	client, err := NewClient(cfg.UploadServiceURL, token)
	if err != nil {
		return fmt.Errorf("failed to connect to upload service: %w", err)
	}
	defer client.Close()
	log.Println("  ✔ Connected to Upload Service")

	// 4. Test Sample File Upload, Stat, Download & Verification
	sampleData := []byte("Default sample data for integration test")
	if cfg.SampleFilePath != "" {
		if fileBytes, readErr := os.ReadFile(cfg.SampleFilePath); readErr == nil {
			sampleData = fileBytes
			log.Printf("[Step 4/7]  Loaded sample file from %s (%d bytes)\n", cfg.SampleFilePath, len(sampleData))
		} else {
			log.Printf("[Step 4/7]  Could not read %s, using fallback sample data (%v)\n", cfg.SampleFilePath, readErr)
		}
	} else {
		log.Printf("[Step 4/7] Using in-memory sample file (%d bytes)\n", len(sampleData))
	}

	samplePath := fmt.Sprintf("test-uploads/sample-%d.txt", time.Now().UnixNano())

	log.Printf("  → Uploading sample file to path: %s ...\n", samplePath)
	upResp, err := client.UploadBytes(ctx, samplePath, "text/plain", sampleData)
	if err != nil {
		return fmt.Errorf("sample upload failed: %w", err)
	}
	log.Printf("  ✔ Upload completed! UploadID: %s, Size: %d bytes\n", upResp.UploadId, upResp.Size)

	log.Printf("  → Statting metadata for: %s ...\n", samplePath)
	statResp, err := client.Stat(ctx, samplePath)
	if err != nil {
		return fmt.Errorf("stat failed: %w", err)
	}
	if statResp.Size != uint64(len(sampleData)) {
		return fmt.Errorf("stat size mismatch: expected %d, got %d", len(sampleData), statResp.Size)
	}
	log.Printf("  ✔ Stat succeeded: Size=%d, ContentType=%s, ModifiedAt=%d\n", statResp.Size, statResp.ContentType, statResp.ModifiedAtUnix)

	log.Printf("  → Downloading full file: %s ...\n", samplePath)
	downloaded, err := client.Download(ctx, samplePath, 0, 0)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	if !bytes.Equal(downloaded, sampleData) {
		return fmt.Errorf("downloaded content mismatch! Length expected %d, got %d", len(sampleData), len(downloaded))
	}
	log.Println("  ✔ Full download verified: contents match sample file exactly")

	// 5. Test Partial / Range Download
	log.Println("[Step 5/7]  Testing byte-range partial download...")
	if len(sampleData) > 20 {
		partialOffset := uint64(5)
		partialLength := uint64(15)
		partialData, err := client.Download(ctx, samplePath, partialOffset, partialLength)
		if err != nil {
			return fmt.Errorf("range download failed: %w", err)
		}
		expectedPartial := sampleData[partialOffset : partialOffset+partialLength]
		if !bytes.Equal(partialData, expectedPartial) {
			return fmt.Errorf("range download data mismatch")
		}
		log.Printf("  ✔ Range download (%d-%d) verified (%d bytes received)\n", partialOffset, partialOffset+partialLength-1, len(partialData))
	} else {
		log.Println("  ✔ Skipped range download (sample data too short)")
	}

	// 6. Test Multi-Part / Multi-MB (> 5MB) Upload
	log.Println("[Step 6/7]  Testing large multipart upload (6MB buffer across chunks)...")
	largePath := fmt.Sprintf("test-uploads/large-file-%d.bin", time.Now().UnixNano())
	largeData := make([]byte, 6*1024*1024)
	_, _ = rand.Read(largeData)
	largeHash := sha256.Sum256(largeData)

	largeResp, err := client.Upload(
		ctx,
		largePath,
		"application/octet-stream",
		bytes.NewReader(largeData),
		uint64(len(largeData)),
		largeHash[:],
		"",
		0,
		512*1024, // 512KB gRPC chunk stream
	)
	if err != nil {
		return fmt.Errorf("multipart upload failed: %w", err)
	}
	log.Printf("  ✔ Multipart upload succeeded! UploadID: %s, Size: %d bytes\n", largeResp.UploadId, largeResp.Size)

	largeStat, err := client.Stat(ctx, largePath)
	if err != nil {
		return fmt.Errorf("stat large file failed: %w", err)
	}
	if largeStat.Size != uint64(len(largeData)) {
		return fmt.Errorf("large file size mismatch: expected %d, got %d", len(largeData), largeStat.Size)
	}
	log.Println("  ✔ Large file metadata verified")

	// 7. Cleanup & Delete Test
	log.Println("[Step 7/7]  Testing file deletion and post-delete 404 verification...")
	if err := client.Delete(ctx, samplePath); err != nil {
		return fmt.Errorf("delete sample file failed: %w", err)
	}
	log.Printf("  ✔ Deleted %s\n", samplePath)

	if err := client.Delete(ctx, largePath); err != nil {
		return fmt.Errorf("delete large file failed: %w", err)
	}
	log.Printf("  ✔ Deleted %s\n", largePath)

	_, err = client.Stat(ctx, samplePath)
	if err == nil {
		return fmt.Errorf("expected 404 NotFound after delete, but file still exists")
	}
	log.Printf("  ✔ Confirmed file deletion: Stat returned %v\n", err)

	log.Println("==========================================================")
	log.Println("🎉 ALL INTEGRATION TESTS PASSED PERFECTLY!")
	log.Println("==========================================================")
	return nil
}
