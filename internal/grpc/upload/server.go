package upload

import (
	uploadv1 "devclub.com/upload/gen/proto/upload/v1"
	"devclub.com/upload/internal/s3"
)

type Server struct {
	uploadv1.UnimplementedFileServiceServer
	s3Client *s3.Client
}

func New(s3Client *s3.Client) *Server {
	return &Server{
		s3Client: s3Client,
	}
}
