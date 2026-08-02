package server

import (
	"context"

	"google.golang.org/grpc"

	userv1 "github.com/soasurs/cordis/gen/user/v1"
)

type fakeUserClient struct {
	userv1.UserServiceClient
	createUserRequest  *userv1.CreateUserRequest
	createUserResponse *userv1.CreateUserResponse
	createUserErr      error
	getUserResponse    *userv1.GetUserResponse
	getUserErr         error

	markEmailVerifiedRequest *userv1.MarkEmailVerifiedRequest
	markEmailVerifiedErr     error
}

func (c *fakeUserClient) MarkEmailVerified(_ context.Context, req *userv1.MarkEmailVerifiedRequest, _ ...grpc.CallOption) (*userv1.MarkEmailVerifiedResponse, error) {
	c.markEmailVerifiedRequest = req
	if c.markEmailVerifiedErr != nil {
		return nil, c.markEmailVerifiedErr
	}
	resp := new(userv1.MarkEmailVerifiedResponse)
	resp.SetOk(true)
	return resp, nil
}

func (c *fakeUserClient) GetUser(_ context.Context, _ *userv1.GetUserRequest, _ ...grpc.CallOption) (*userv1.GetUserResponse, error) {
	if c.getUserErr != nil {
		return nil, c.getUserErr
	}
	return c.getUserResponse, nil
}

func (c *fakeUserClient) CreateUser(_ context.Context, req *userv1.CreateUserRequest, _ ...grpc.CallOption) (*userv1.CreateUserResponse, error) {
	c.createUserRequest = req
	if c.createUserErr != nil {
		return nil, c.createUserErr
	}
	return c.createUserResponse, nil
}
