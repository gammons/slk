package messages

// SixelAvatar returns userID's sixel avatar from fn for RenderOptions
// (AvatarSixelKey, AvatarSixel): empty when fn is nil or has none.
// Both panes call it beside their AvatarFunc.
func SixelAvatar(fn SixelAvatarFunc, userID string) (key string, bytes []byte) {
	if fn == nil {
		return "", nil
	}
	key, bytes, ok := fn(userID)
	if !ok {
		return "", nil
	}
	return key, bytes
}
