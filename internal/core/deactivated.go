package core

// IsHiddenPeerConversation reports whether a conversation is a one-to-one DM
// or app DM whose peer is in hidden, the set of deactivated users to hide.
// It is the one rule the sidebar, the channel finder and the "#" autocomplete
// share. Group DMs and channels have no single peer, so they are never
// hidden by it, and a nil set hides nothing.
func IsHiddenPeerConversation(convType, dmUserID string, hidden map[string]bool) bool {
	return (convType == "dm" || convType == "app") && dmUserID != "" && hidden[dmUserID]
}
