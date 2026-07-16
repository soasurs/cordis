package server

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGuildMemberRemovedEventIsDeliveredBeforeUnsubscribe(t *testing.T) {
	gateway := newTestGatewayWithGuild(&fakeAuthenticatorClient{}, &fakePresenceClient{}, &fakeGuildClient{})
	c := testEventClient(gateway, 1002)
	gateway.hub.add(c)
	gateway.hub.subscribeGuild(c, 10)

	err := gateway.handleGuildEvent(t.Context(), []byte(`{
		"t":"guild.member.removed",
		"d":{"guild_id":"10","user_id":"1002","revision":2,"removed_at":100}
	}`))
	require.NoError(t, err)
	msg := <-c.send
	require.Equal(t, "guild.member.removed", msg.T)
	require.Empty(t, gateway.hub.guildClients(10))
}

func TestGuildChannelEventChecksViewPermission(t *testing.T) {
	guild := &fakeGuildClient{deniedChannels: map[int64]bool{20: true}}
	gateway := newTestGatewayWithGuild(&fakeAuthenticatorClient{}, &fakePresenceClient{}, guild)
	c := testEventClient(gateway, 1001)
	gateway.hub.add(c)
	gateway.hub.subscribeGuild(c, 10)

	err := gateway.handleGuildEvent(t.Context(), []byte(`{
		"t":"guild.channel.updated",
		"d":{"id":"20","guild_id":"10","name":"private","type":1}
	}`))
	require.NoError(t, err)
	require.Empty(t, c.send)

	guild.deniedChannels[20] = false
	err = gateway.handleGuildEvent(t.Context(), []byte(`{
		"t":"guild.channel.updated",
		"d":{"id":"20","guild_id":"10","name":"visible","type":1}
	}`))
	require.NoError(t, err)
	msg := <-c.send
	require.Equal(t, "guild.channel.updated", msg.T)
	var payload map[string]any
	require.NoError(t, json.Unmarshal(msg.D, &payload))
	require.Equal(t, "visible", payload["name"])
}

func testEventClient(gateway *Server, userID int64) *client {
	return &client{
		server: gateway, userID: userID, gatewaySessionID: "test",
		channels: make(map[int64]struct{}), guilds: make(map[int64]struct{}),
		send: make(chan envelope, 4), done: make(chan struct{}),
	}
}
