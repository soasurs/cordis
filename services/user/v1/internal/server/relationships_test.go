package server

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	userv1 "github.com/soasurs/cordis/gen/user/v1"
	"github.com/soasurs/cordis/pkg/rpcerror"
	"github.com/soasurs/cordis/pkg/snowflake"
	"github.com/soasurs/cordis/services/user/v1/config"
	"github.com/soasurs/cordis/services/user/v1/internal/model"
	"github.com/soasurs/cordis/services/user/v1/internal/svc"
)

type publishedUserRecord struct {
	key     string
	payload []byte
}

type fakeUserPublisher struct {
	records []publishedUserRecord
}

func (p *fakeUserPublisher) Publish(_ context.Context, key, payload []byte) error {
	p.records = append(p.records, publishedUserRecord{
		key:     string(key),
		payload: append([]byte(nil), payload...),
	})
	return nil
}

func (p *fakeUserPublisher) reset() {
	p.records = nil
}

func decodeRelationshipEvent(t *testing.T, record publishedUserRecord) (string, relationshipPayload) {
	t.Helper()
	var envelope eventEnvelope[relationshipPayload]
	require.NoError(t, json.Unmarshal(record.payload, &envelope))
	return envelope.Type, envelope.Data
}

func newRelationshipTestServer(t *testing.T, fake *fakeStore, publisher svc.EventPublisher) userv1.UserServiceServer {
	t.Helper()
	node, err := snowflake.New()
	require.NoError(t, err)
	return New(&svc.ServiceContext{
		Cfg:       config.Config{Kafka: config.KafkaConfig{PublishTimeoutMs: 100}},
		Store:     fake,
		Snowflake: node,
		Publisher: publisher,
	})
}

// relationshipTestStore seeds the target user so existence checks pass.
func relationshipTestStore() *fakeStore {
	fake := newFakeStore()
	fake.user = &model.User{UserID: 2002, Email: "target@example.com"}
	return fake
}

func pairKey(userID, targetID int64) [2]int64 {
	return [2]int64{userID, targetID}
}

func TestSendFriendRequestCreatesPendingPair(t *testing.T) {
	fake := relationshipTestStore()
	publisher := new(fakeUserPublisher)
	server := newRelationshipTestServer(t, fake, publisher)

	req := new(userv1.SendFriendRequestRequest)
	req.SetUserId(1001)
	req.SetTargetId(2002)
	resp, err := server.SendFriendRequest(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, userv1.RelationshipType_RELATIONSHIP_TYPE_OUTGOING, resp.GetRelationship().GetType())

	require.Equal(t, model.RelationshipOutgoing, fake.relationships[pairKey(1001, 2002)].Type)
	require.Equal(t, model.RelationshipIncoming, fake.relationships[pairKey(2002, 1001)].Type)
	require.Equal(t, [][2]int64{{1001, 2002}}, fake.lockedPairs)

	require.Len(t, publisher.records, 2)
	require.Equal(t, "1001", publisher.records[0].key)
	eventType, payload := decodeRelationshipEvent(t, publisher.records[0])
	require.Equal(t, EventTypeRelationshipUpdated, eventType)
	require.Equal(t, "1001", payload.UserID)
	require.Equal(t, "2002", payload.TargetID)
	require.Equal(t, model.RelationshipOutgoing, payload.Type)

	require.Equal(t, "2002", publisher.records[1].key)
	eventType, payload = decodeRelationshipEvent(t, publisher.records[1])
	require.Equal(t, EventTypeRelationshipUpdated, eventType)
	require.Equal(t, model.RelationshipIncoming, payload.Type)

	// Repeating the pending request is a silent no-op.
	publisher.reset()
	resp, err = server.SendFriendRequest(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, userv1.RelationshipType_RELATIONSHIP_TYPE_OUTGOING, resp.GetRelationship().GetType())
	require.Empty(t, publisher.records)
}

func TestSendFriendRequestMutualIntentBecomesFriendship(t *testing.T) {
	fake := relationshipTestStore()
	// The target already asked first: 1001 holds an incoming request.
	fake.relationships[pairKey(1001, 2002)] = &model.Relationship{UserID: 1001, TargetID: 2002, Type: model.RelationshipIncoming, CreatedAt: 1}
	fake.relationships[pairKey(2002, 1001)] = &model.Relationship{UserID: 2002, TargetID: 1001, Type: model.RelationshipOutgoing, CreatedAt: 1}
	publisher := new(fakeUserPublisher)
	server := newRelationshipTestServer(t, fake, publisher)

	req := new(userv1.SendFriendRequestRequest)
	req.SetUserId(1001)
	req.SetTargetId(2002)
	resp, err := server.SendFriendRequest(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, userv1.RelationshipType_RELATIONSHIP_TYPE_FRIEND, resp.GetRelationship().GetType())
	require.Equal(t, model.RelationshipFriend, fake.relationships[pairKey(1001, 2002)].Type)
	require.Equal(t, model.RelationshipFriend, fake.relationships[pairKey(2002, 1001)].Type)
	require.Len(t, publisher.records, 2)
}

func TestSendFriendRequestRejectsBlockedAndDuplicates(t *testing.T) {
	fake := relationshipTestStore()
	server := newRelationshipTestServer(t, fake, nil)

	req := new(userv1.SendFriendRequestRequest)
	req.SetUserId(1001)
	req.SetTargetId(2002)

	// Blocked by the caller.
	fake.relationships[pairKey(1001, 2002)] = &model.Relationship{UserID: 1001, TargetID: 2002, Type: model.RelationshipBlocked, CreatedAt: 1}
	_, err := server.SendFriendRequest(context.Background(), req)
	require.Equal(t, codes.PermissionDenied, status.Code(err))
	require.True(t, rpcerror.Is(err, rpcerror.UserDomain, rpcerror.UserRelationshipBlocked))

	// Blocked by the target.
	delete(fake.relationships, pairKey(1001, 2002))
	fake.relationships[pairKey(2002, 1001)] = &model.Relationship{UserID: 2002, TargetID: 1001, Type: model.RelationshipBlocked, CreatedAt: 1}
	_, err = server.SendFriendRequest(context.Background(), req)
	require.Equal(t, codes.PermissionDenied, status.Code(err))

	// Existing friendship.
	delete(fake.relationships, pairKey(2002, 1001))
	fake.relationships[pairKey(1001, 2002)] = &model.Relationship{UserID: 1001, TargetID: 2002, Type: model.RelationshipFriend, CreatedAt: 1}
	_, err = server.SendFriendRequest(context.Background(), req)
	require.Equal(t, codes.AlreadyExists, status.Code(err))
	require.True(t, rpcerror.Is(err, rpcerror.UserDomain, rpcerror.UserRelationshipAlreadyExists))

	// Unknown target.
	delete(fake.relationships, pairKey(1001, 2002))
	req.SetTargetId(9999)
	_, err = server.SendFriendRequest(context.Background(), req)
	require.Equal(t, codes.NotFound, status.Code(err))

	// Self target.
	req.SetTargetId(1001)
	_, err = server.SendFriendRequest(context.Background(), req)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestAcceptFriendRequest(t *testing.T) {
	fake := relationshipTestStore()
	fake.relationships[pairKey(1001, 2002)] = &model.Relationship{UserID: 1001, TargetID: 2002, Type: model.RelationshipIncoming, CreatedAt: 1}
	fake.relationships[pairKey(2002, 1001)] = &model.Relationship{UserID: 2002, TargetID: 1001, Type: model.RelationshipOutgoing, CreatedAt: 1}
	publisher := new(fakeUserPublisher)
	server := newRelationshipTestServer(t, fake, publisher)

	req := new(userv1.AcceptFriendRequestRequest)
	req.SetUserId(1001)
	req.SetTargetId(2002)
	resp, err := server.AcceptFriendRequest(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, userv1.RelationshipType_RELATIONSHIP_TYPE_FRIEND, resp.GetRelationship().GetType())
	require.Equal(t, model.RelationshipFriend, fake.relationships[pairKey(2002, 1001)].Type)
	require.Len(t, publisher.records, 2)

	// Accepting without a pending incoming request fails.
	req.SetTargetId(2002)
	req.SetUserId(3003)
	_, err = server.AcceptFriendRequest(context.Background(), req)
	require.Equal(t, codes.NotFound, status.Code(err))
	require.True(t, rpcerror.Is(err, rpcerror.UserDomain, rpcerror.UserRelationshipNotFound))
}

func TestDeclineFriendRequestRemovesPair(t *testing.T) {
	fake := relationshipTestStore()
	fake.relationships[pairKey(1001, 2002)] = &model.Relationship{UserID: 1001, TargetID: 2002, Type: model.RelationshipIncoming, CreatedAt: 1}
	fake.relationships[pairKey(2002, 1001)] = &model.Relationship{UserID: 2002, TargetID: 1001, Type: model.RelationshipOutgoing, CreatedAt: 1}
	publisher := new(fakeUserPublisher)
	server := newRelationshipTestServer(t, fake, publisher)

	req := new(userv1.DeclineFriendRequestRequest)
	req.SetUserId(1001)
	req.SetTargetId(2002)
	resp, err := server.DeclineFriendRequest(context.Background(), req)
	require.NoError(t, err)
	require.True(t, resp.GetOk())
	require.Empty(t, fake.relationships)

	require.Len(t, publisher.records, 2)
	eventType, _ := decodeRelationshipEvent(t, publisher.records[0])
	require.Equal(t, EventTypeRelationshipRemoved, eventType)
}

func TestRemoveFriendHandlesEveryPendingShape(t *testing.T) {
	fake := relationshipTestStore()
	server := newRelationshipTestServer(t, fake, nil)

	req := new(userv1.RemoveFriendRequest)
	req.SetUserId(1001)
	req.SetTargetId(2002)

	for _, relationshipType := range []int16{model.RelationshipFriend, model.RelationshipOutgoing, model.RelationshipIncoming} {
		fake.relationships[pairKey(1001, 2002)] = &model.Relationship{UserID: 1001, TargetID: 2002, Type: relationshipType, CreatedAt: 1}
		reverse := map[int16]int16{
			model.RelationshipFriend:   model.RelationshipFriend,
			model.RelationshipOutgoing: model.RelationshipIncoming,
			model.RelationshipIncoming: model.RelationshipOutgoing,
		}[relationshipType]
		fake.relationships[pairKey(2002, 1001)] = &model.Relationship{UserID: 2002, TargetID: 1001, Type: reverse, CreatedAt: 1}

		resp, err := server.RemoveFriend(context.Background(), req)
		require.NoError(t, err)
		require.True(t, resp.GetOk())
		require.Empty(t, fake.relationships)
	}

	// A block is not removable through RemoveFriend.
	fake.relationships[pairKey(1001, 2002)] = &model.Relationship{UserID: 1001, TargetID: 2002, Type: model.RelationshipBlocked, CreatedAt: 1}
	_, err := server.RemoveFriend(context.Background(), req)
	require.Equal(t, codes.NotFound, status.Code(err))
}

func TestBlockUserStripsReverseAndStaysPrivate(t *testing.T) {
	fake := relationshipTestStore()
	fake.relationships[pairKey(1001, 2002)] = &model.Relationship{UserID: 1001, TargetID: 2002, Type: model.RelationshipFriend, CreatedAt: 1}
	fake.relationships[pairKey(2002, 1001)] = &model.Relationship{UserID: 2002, TargetID: 1001, Type: model.RelationshipFriend, CreatedAt: 1}
	publisher := new(fakeUserPublisher)
	server := newRelationshipTestServer(t, fake, publisher)

	req := new(userv1.BlockUserRequest)
	req.SetUserId(1001)
	req.SetTargetId(2002)
	resp, err := server.BlockUser(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, userv1.RelationshipType_RELATIONSHIP_TYPE_BLOCKED, resp.GetRelationship().GetType())
	require.Equal(t, model.RelationshipBlocked, fake.relationships[pairKey(1001, 2002)].Type)
	require.Nil(t, fake.relationships[pairKey(2002, 1001)])

	// The blocker sees the block; the blocked side only sees a removal.
	require.Len(t, publisher.records, 2)
	require.Equal(t, "1001", publisher.records[0].key)
	eventType, payload := decodeRelationshipEvent(t, publisher.records[0])
	require.Equal(t, EventTypeRelationshipUpdated, eventType)
	require.Equal(t, model.RelationshipBlocked, payload.Type)
	require.Equal(t, "2002", publisher.records[1].key)
	eventType, _ = decodeRelationshipEvent(t, publisher.records[1])
	require.Equal(t, EventTypeRelationshipRemoved, eventType)

	// Re-blocking is a no-op.
	publisher.reset()
	_, err = server.BlockUser(context.Background(), req)
	require.NoError(t, err)
	require.Empty(t, publisher.records)
}

func TestBlockUserKeepsMutualBlock(t *testing.T) {
	fake := relationshipTestStore()
	fake.relationships[pairKey(2002, 1001)] = &model.Relationship{UserID: 2002, TargetID: 1001, Type: model.RelationshipBlocked, CreatedAt: 1}
	publisher := new(fakeUserPublisher)
	server := newRelationshipTestServer(t, fake, publisher)

	req := new(userv1.BlockUserRequest)
	req.SetUserId(1001)
	req.SetTargetId(2002)
	_, err := server.BlockUser(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, model.RelationshipBlocked, fake.relationships[pairKey(1001, 2002)].Type)
	require.Equal(t, model.RelationshipBlocked, fake.relationships[pairKey(2002, 1001)].Type)

	// Only the blocker learns anything.
	require.Len(t, publisher.records, 1)
	require.Equal(t, "1001", publisher.records[0].key)
}

func TestUnblockUserRemovesOwnRowOnly(t *testing.T) {
	fake := relationshipTestStore()
	fake.relationships[pairKey(1001, 2002)] = &model.Relationship{UserID: 1001, TargetID: 2002, Type: model.RelationshipBlocked, CreatedAt: 1}
	fake.relationships[pairKey(2002, 1001)] = &model.Relationship{UserID: 2002, TargetID: 1001, Type: model.RelationshipBlocked, CreatedAt: 1}
	publisher := new(fakeUserPublisher)
	server := newRelationshipTestServer(t, fake, publisher)

	req := new(userv1.UnblockUserRequest)
	req.SetUserId(1001)
	req.SetTargetId(2002)
	resp, err := server.UnblockUser(context.Background(), req)
	require.NoError(t, err)
	require.True(t, resp.GetOk())
	require.Nil(t, fake.relationships[pairKey(1001, 2002)])
	require.Equal(t, model.RelationshipBlocked, fake.relationships[pairKey(2002, 1001)].Type)
	require.Len(t, publisher.records, 1)

	// Unblocking without a block fails.
	_, err = server.UnblockUser(context.Background(), req)
	require.Equal(t, codes.NotFound, status.Code(err))
}

func TestListRelationshipsFiltersAndPaginates(t *testing.T) {
	fake := relationshipTestStore()
	fake.relationships[pairKey(1001, 2002)] = &model.Relationship{UserID: 1001, TargetID: 2002, Type: model.RelationshipFriend, CreatedAt: 1}
	fake.relationships[pairKey(1001, 2003)] = &model.Relationship{UserID: 1001, TargetID: 2003, Type: model.RelationshipFriend, CreatedAt: 1}
	fake.relationships[pairKey(1001, 2004)] = &model.Relationship{UserID: 1001, TargetID: 2004, Type: model.RelationshipBlocked, CreatedAt: 1}
	server := newRelationshipTestServer(t, fake, nil)

	req := new(userv1.ListRelationshipsRequest)
	req.SetUserId(1001)
	resp, err := server.ListRelationships(context.Background(), req)
	require.NoError(t, err)
	require.Len(t, resp.GetRelationships(), 3)
	require.Equal(t, int64(2004), resp.GetRelationships()[0].GetTargetId())
	require.Equal(t, int64(2002), resp.GetBeforeTargetId())

	req.SetType(userv1.RelationshipType_RELATIONSHIP_TYPE_FRIEND)
	resp, err = server.ListRelationships(context.Background(), req)
	require.NoError(t, err)
	require.Len(t, resp.GetRelationships(), 2)

	req.SetType(userv1.RelationshipType_RELATIONSHIP_TYPE_UNSPECIFIED)
	req.SetBeforeTargetId(2004)
	req.SetLimit(1)
	resp, err = server.ListRelationships(context.Background(), req)
	require.NoError(t, err)
	require.Len(t, resp.GetRelationships(), 1)
	require.Equal(t, int64(2003), resp.GetRelationships()[0].GetTargetId())
}

func TestCheckRelationships(t *testing.T) {
	fake := relationshipTestStore()
	fake.relationships[pairKey(1001, 2002)] = &model.Relationship{UserID: 1001, TargetID: 2002, Type: model.RelationshipBlocked, CreatedAt: 1}
	server := newRelationshipTestServer(t, fake, nil)

	req := new(userv1.CheckRelationshipsRequest)
	req.SetUserId(1001)
	req.SetTargetIds([]int64{2002, 2003})
	resp, err := server.CheckRelationships(context.Background(), req)
	require.NoError(t, err)
	require.Len(t, resp.GetRelationships(), 1)
	require.Equal(t, int64(2002), resp.GetRelationships()[0].GetTargetId())

	req.SetTargetIds(nil)
	resp, err = server.CheckRelationships(context.Background(), req)
	require.NoError(t, err)
	require.Empty(t, resp.GetRelationships())

	tooMany := make([]int64, maxRelationshipBatch+1)
	for i := range tooMany {
		tooMany[i] = int64(3000 + i)
	}
	req.SetTargetIds(tooMany)
	_, err = server.CheckRelationships(context.Background(), req)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}
