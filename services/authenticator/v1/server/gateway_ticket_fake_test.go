package server

import (
	"context"
	"time"

	"github.com/soasurs/cordis/services/authenticator/v1/internal/gatewayticket"
)

type fakeGatewayTicketStore struct {
	tickets map[string]gatewayticket.Ticket
}

func newFakeGatewayTicketStore() *fakeGatewayTicketStore {
	return &fakeGatewayTicketStore{tickets: make(map[string]gatewayticket.Ticket)}
}

func (s *fakeGatewayTicketStore) Put(_ context.Context, tokenHash string, ticket gatewayticket.Ticket, _ time.Duration) error {
	s.tickets[tokenHash] = ticket
	return nil
}

func (s *fakeGatewayTicketStore) Redeem(_ context.Context, tokenHash string) (gatewayticket.Ticket, error) {
	ticket, ok := s.tickets[tokenHash]
	if !ok {
		return gatewayticket.Ticket{}, gatewayticket.ErrNotFound
	}
	delete(s.tickets, tokenHash)
	return ticket, nil
}
