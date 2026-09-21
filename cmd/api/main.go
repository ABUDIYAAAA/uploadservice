package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	uploadv1 "devclub.com/upload/gen/proto/upload/v1"
	"devclub.com/upload/internal/auth"
	"devclub.com/upload/internal/config"
	"devclub.com/upload/internal/grpc/upload"
	"devclub.com/upload/internal/s3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

func main() {
	cfg, err := config.NewConfig()
	if err != nil {
		log.Fatalln("Error loading up config:", err)
	}

	lis, err := net.Listen("tcp", fmt.Sprintf(":%s", cfg.Port))
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}

	ctx := context.Background()
	s3Client, err := s3.NewClient(ctx, *cfg)
	if err != nil {
		log.Fatalf("Failed to initialize S3 client: %v", err)
	}

	identityClient := auth.NewClient(cfg.IdentityServiceURL, cfg.ServiceID, cfg.ServiceSecret)
	authInterceptor := auth.NewInterceptor(identityClient)

	grpcServer := grpc.NewServer(
		grpc.UnaryInterceptor(authInterceptor.Unary),
		grpc.StreamInterceptor(authInterceptor.Stream),
	)

	uploadServer := upload.New(s3Client)

	uploadv1.RegisterFileServiceServer(
		grpcServer,
		uploadServer,
	)

	reflection.Register(grpcServer)

	go func() {
		log.Println("gRPC server listening on:", cfg.Port)
		if err := grpcServer.Serve(lis); err != nil {
			log.Fatalf("Failed to start grpc server: %v", err)
		}
	}()

	exit := make(chan os.Signal, 1)
	signal.Notify(exit, os.Interrupt, syscall.SIGTERM)
	<-exit

	log.Println("Exiting grpc server...")
	grpcServer.GracefulStop()
	log.Println("Exited grpc server")
}
