package rpcerror

import (
	"testing"

	"google.golang.org/grpc/codes"
)

func TestNewAndParse(t *testing.T) {
	err := New(codes.AlreadyExists, UserDomain, UserEmailAlreadyExists, "email already exists")

	if Code(err) != codes.AlreadyExists {
		t.Fatalf("Code() = %v, want %v", Code(err), codes.AlreadyExists)
	}

	info, ok := Parse(err)
	if !ok {
		t.Fatal("expected error info")
	}
	if info.Domain != UserDomain || info.Reason != UserEmailAlreadyExists {
		t.Fatalf("unexpected error info: %+v", info)
	}
	if !Is(err, UserDomain, UserEmailAlreadyExists) {
		t.Fatal("expected Is to match")
	}
}
