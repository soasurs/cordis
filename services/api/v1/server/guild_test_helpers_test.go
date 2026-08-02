package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	apiv1connect "github.com/soasurs/cordis/gen/api/v1/apiv1connect"
	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	"github.com/soasurs/cordis/services/api/v1/svc"
)

func newGuildHTTPClient(t *testing.T, guildClient *fakeGuildClient) (apiv1connect.GuildServiceClient, func()) {
	return newGuildHTTPClientWithUser(t, guildClient, new(fakeUserClient))
}

func newGuildHTTPClientWithUser(
	t *testing.T,
	guildClient *fakeGuildClient,
	userClient *fakeUserClient,
) (apiv1connect.GuildServiceClient, func()) {
	t.Helper()
	svcCtx := &svc.ServiceContext{
		AuthenticatorClient: &fakeAuthenticatorClient{verifyResponse: verifyAccessTokenResponse(1001)},
		UserClient:          userClient,
		GuildClient:         guildClient,
	}
	path, handler := apiv1connect.NewGuildServiceHandler(NewGuild(svcCtx))
	mux := http.NewServeMux()
	mux.Handle(path, handler)
	httpServer := httptest.NewServer(mux)
	httpClient := &http.Client{Transport: bearerRoundTripper{base: http.DefaultTransport, accessToken: "access-token"}}
	return apiv1connect.NewGuildServiceClient(httpClient, httpServer.URL), httpServer.Close
}

func internalGuild() *guildv1.Guild {
	guild := new(guildv1.Guild)
	guild.SetId(3001)
	guild.SetOwnerId(1001)
	guild.SetName("Cordis")
	guild.SetDescription("Community description")
	guild.SetIconAssetId(6001)
	guild.SetRevision(1)
	guild.SetCreatedAt(4001)
	return guild
}

func internalGuildMember() *guildv1.GuildMember {
	member := new(guildv1.GuildMember)
	member.SetGuildId(3001)
	member.SetUserId(1001)
	member.SetNickname("member")
	member.SetRevision(2)
	member.SetJoinedAt(4001)
	member.SetUpdatedAt(4002)
	return member
}

func internalGuildRole() *guildv1.GuildRole {
	role := new(guildv1.GuildRole)
	role.SetId(4001)
	role.SetGuildId(3001)
	role.SetName("moderator")
	role.SetPermissions(16)
	role.SetPosition(1)
	role.SetRevision(3)
	role.SetCreatedAt(4000)
	role.SetUpdatedAt(4001)
	return role
}

func internalGuildChannel() *guildv1.GuildChannel {
	channel := new(guildv1.GuildChannel)
	channel.SetId(5001)
	channel.SetGuildId(3001)
	channel.SetName("general")
	channel.SetType(guildv1.GuildChannelType_GUILD_CHANNEL_TYPE_TEXT)
	channel.SetPosition(0)
	channel.SetTopic("topic")
	channel.SetRevision(4)
	channel.SetCreatedAt(4000)
	channel.SetUpdatedAt(4001)
	channel.SetParentId(0)
	return channel
}
