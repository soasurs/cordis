package rpcerror

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
)

func TestNewAndParse(t *testing.T) {
	err := New(codes.AlreadyExists, UserDomain, UserEmailAlreadyExists, "email already exists")

	require.Equal(t, codes.AlreadyExists, Code(err))

	info, ok := Parse(err)
	require.True(t, ok, "expected error info")
	require.Equal(t, UserDomain, info.Domain)
	require.Equal(t, UserEmailAlreadyExists, info.Reason)

	require.True(t, Is(err, UserDomain, UserEmailAlreadyExists), "expected Is to match")
}
