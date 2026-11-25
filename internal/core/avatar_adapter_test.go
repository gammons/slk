package core

import "testing"

// NewSixelAvatarService hands out sixel avatars through the optional
// SixelAvatarService; without a sixel source it is a plain
// AvatarService, so the App keeps avatars half-block.
func TestNewSixelAvatarService(t *testing.T) {
	half := func(string) string { return "HB" }
	s := NewSixelAvatarService(half, nil, func(userID string) (string, []byte, bool) {
		return "avatar-" + userID, []byte{1, 2}, true
	})
	sx, ok := s.(SixelAvatarService)
	if !ok {
		t.Fatal("service with a sixel source is not a SixelAvatarService")
	}
	if key, b, ok := sx.SixelAvatar("U1"); !ok || key != "avatar-U1" || len(b) != 2 {
		t.Errorf("SixelAvatar(U1) = (%q, %v, %v)", key, b, ok)
	}
	if got := s.Avatar("U1"); got != "HB" {
		t.Errorf("Avatar(U1) = %q, want the half-block avatar", got)
	}
	if _, ok := NewSixelAvatarService(half, nil, nil).(SixelAvatarService); ok {
		t.Error("service without a sixel source claims to be a SixelAvatarService")
	}
}
