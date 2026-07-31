package mention

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseUsersAndRoles(t *testing.T) {
	set := Parse("hi <@100> and <@&200> and <@!300>")
	require.Equal(t, []int64{100, 300}, set.UserIDs)
	require.Equal(t, []int64{200}, set.RoleIDs)
	require.False(t, set.Everyone)
}

func TestParseDeduplicatesAndKeepsFirstAppearanceOrder(t *testing.T) {
	set := Parse("<@200> <@100> <@200> <@&300> <@&300>")
	require.Equal(t, []int64{200, 100}, set.UserIDs)
	require.Equal(t, []int64{300}, set.RoleIDs)
}

func TestParseEveryone(t *testing.T) {
	require.True(t, Parse("@everyone").Everyone)
	require.True(t, Parse("hi @everyone!").Everyone)
	require.True(t, Parse("(@everyone)").Everyone)

	require.False(t, Parse("@Everyone").Everyone)
	require.False(t, Parse("@everyone1").Everyone)
	require.False(t, Parse("@everyone_").Everyone)
	require.False(t, Parse("x@everyone").Everyone)
}

func TestParseIgnoresMalformedMarkup(t *testing.T) {
	for _, content := range []string{
		"<@>",
		"<@abc>",
		"<@-1>",
		"<@0>",
		"<@99999999999999999999999>",
		"<@&>",
		"<@&abc>",
		"<123>",
		"@everyonex",
	} {
		set := Parse(content)
		require.Empty(t, set.UserIDs, content)
		require.Empty(t, set.RoleIDs, content)
		require.False(t, set.Everyone, content)
	}
}

func TestParseEscapedMentionsStayPlainText(t *testing.T) {
	set := Parse(`\<@100> \@everyone`)
	require.Empty(t, set.UserIDs)
	require.False(t, set.Everyone)

	// Two backslashes are an escaped backslash, so the mention still parses.
	set = Parse(`\\<@100>`)
	require.Equal(t, []int64{100}, set.UserIDs)
}

func TestParseEmptyAndAttachmentOnly(t *testing.T) {
	set := Parse("")
	require.Empty(t, set.UserIDs)
	require.Empty(t, set.RoleIDs)
	require.False(t, set.Everyone)

	set = Parse("plain text only")
	require.Empty(t, set.UserIDs)
	require.Empty(t, set.RoleIDs)
	require.False(t, set.Everyone)
}
