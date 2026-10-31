package boot_test

import (
	"context"
	"net/url"
	"slices"
	"testing"

	slackclient "github.com/gammons/slk/internal/slack"
	"github.com/gammons/slk/internal/slack/boot"
)

func TestUserBoot_MutePrefsReachParser(t *testing.T) {
	// Exercise both JSON layers: userBoot returns notification prefs as an
	// encoded string, which the existing Slack mute parser then interprets.
	const body = `{"ok":true,"prefs":{"muted_channels":"C_LEGACY","all_notifications_prefs":"{\"channels\":{\"C_MUTED\":{\"muted\":true},\"C_UNMUTED\":{\"muted\":false},\"C_ALSO_MUTED\":{\"muted\":true}}}"}}`
	res, err := boot.UserBoot(context.Background(), func(context.Context, string, url.Values) ([]byte, error) {
		return []byte(body), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	got := slackclient.ParseMutedFromAllNotificationsPrefs(res.Prefs.AllNotificationsPrefs)
	slices.Sort(got)
	want := []string{"C_ALSO_MUTED", "C_MUTED"}
	if !slices.Equal(got, want) {
		t.Errorf("muted channels = %q; want %q", got, want)
	}
}
