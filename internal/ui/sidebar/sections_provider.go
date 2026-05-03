package sidebar

// SectionsProvider supplies Slack-native sidebar sections to the model.
// When non-nil and Ready returns true, the model uses provider data
// instead of the config-glob path. Implementations live in the service
// layer (SectionStore); this interface keeps the sidebar package free
// of cross-package dependencies.
type SectionsProvider interface {
	Ready() bool
	// OrderedSlackSections returns sections in the order they should
	// render, already filtered to the renderable set. Each entry is
	// the data the sidebar needs for the header row.
	OrderedSlackSections() []SectionMeta
}

// SectionMeta is the sidebar's view of one Slack section.
type SectionMeta struct {
	ID    string
	Name  string
	Emoji string // shortcode like "orange_book"; empty for none
	Type  string // standard | channels | direct_messages | recent_apps
}
