package server

import (
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var (
	errGatewayIDRequired  = status.Error(codes.InvalidArgument, "gateway id is required")
	errGenerationRequired = status.Error(codes.InvalidArgument, "generation is required")
	errRPCAddrRequired    = status.Error(codes.InvalidArgument, "rpc addr is required")
	errChannelIDRequired  = status.Error(codes.InvalidArgument, "channel id is required")
)
