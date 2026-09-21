package main

import (
	"context"
	"flag"
	"log"
	"os"

	"devclub.com/upload/test"
	"github.com/joho/godotenv"
)

func main() {
	_ = godotenv.Load()

	identityURL := flag.String("identity-url", getEnvOrDefault("IDENTITY_SERVICE_URL", "http://localhost:8080"), "Identity service URL")
	uploadURL := flag.String("upload-url", getEnvOrDefault("UPLOAD_SERVICE_URL", "localhost:50051"), "Upload service gRPC target")
	clientID := flag.String("client-id", getEnvOrDefault("TEST_ID", os.Getenv("SERVICE_ID")), "Calling service client ID")
	clientSecret := flag.String("client-secret", getEnvOrDefault("TEST_SECRET", os.Getenv("SERVICE_SECRET")), "Calling service client secret")
	sampleFile := flag.String("file", "test/sample_file.txt", "Sample file path to upload")

	flag.Parse()

	if *clientID == "" || *clientSecret == "" {
		log.Fatalln("Error: client-id or client-secret not found. Ensure TEST_ID and TEST_SECRET are in .env or passed via flags.")
	}

	cfg := test.TestConfig{
		IdentityServiceURL: *identityURL,
		UploadServiceURL:   *uploadURL,
		ClientID:           *clientID,
		ClientSecret:       *clientSecret,
		SampleFilePath:     *sampleFile,
	}

	ctx := context.Background()
	if err := test.RunSuite(ctx, cfg); err != nil {
		log.Fatalf("❌ Test suite failed: %v", err)
	}
}

func getEnvOrDefault(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}
