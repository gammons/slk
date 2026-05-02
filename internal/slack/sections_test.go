package slackclient

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestDecodeSection_REST(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "sections_rest_truelist.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var resp channelSectionsListResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !resp.OK {
		t.Fatalf("ok=false")
	}
	// Find the "automated22" standard section.
	var got *SidebarSection
	for i := range resp.Sections {
		if resp.Sections[i].Name == "automated22" {
			got = &resp.Sections[i]
			break
		}
	}
	if got == nil {
		t.Fatalf("automated22 not found")
	}
	if got.Type != "standard" {
		t.Errorf("Type = %q, want standard", got.Type)
	}
	if got.Next != "L0B12LBBCTD" {
		t.Errorf("Next = %q, want L0B12LBBCTD", got.Next)
	}
	if got.LastUpdate != 1777720328 {
		t.Errorf("LastUpdate = %d, want 1777720328", got.LastUpdate)
	}
	wantChans := []string{"C054JFCBN69", "D09R4P6G6QL"}
	if len(got.ChannelIDs) != len(wantChans) {
		t.Fatalf("ChannelIDs len = %d, want %d", len(got.ChannelIDs), len(wantChans))
	}
	for i, c := range wantChans {
		if got.ChannelIDs[i] != c {
			t.Errorf("ChannelIDs[%d] = %q, want %q", i, got.ChannelIDs[i], c)
		}
	}
	if got.ChannelsCount != 2 {
		t.Errorf("ChannelsCount = %d, want 2", got.ChannelsCount)
	}
	if got.ChannelsCursor != "D09R4P6G6QL" {
		t.Errorf("ChannelsCursor = %q, want D09R4P6G6QL", got.ChannelsCursor)
	}
	if got.Emoji != "" {
		t.Errorf("Emoji = %q, want empty", got.Emoji)
	}
	if got.IsRedacted {
		t.Errorf("IsRedacted = true, want false")
	}
}

func TestDecodeSection_TailHasEmptyNext(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "sections_rest_truelist.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var resp channelSectionsListResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	var got *SidebarSection
	for i := range resp.Sections {
		if resp.Sections[i].Name == "Agents" {
			got = &resp.Sections[i]
			break
		}
	}
	if got == nil {
		t.Fatalf("Agents section not found")
	}
	if got.Next != "" {
		t.Errorf("Next = %q, want empty (null in JSON)", got.Next)
	}
	if got.Type != "agents" {
		t.Errorf("Type = %q, want agents", got.Type)
	}
	if got.ChannelsCursor != "" {
		t.Errorf("ChannelsCursor = %q, want empty", got.ChannelsCursor)
	}
}

func TestDecodeSection_RedactedSection(t *testing.T) {
	data := []byte(`{
		"channel_section_id": "L_R",
		"name": "Hidden",
		"type": "standard",
		"emoji": "",
		"next_channel_section_id": null,
		"last_updated": 1700000000,
		"channel_ids_page": {"channel_ids": [], "count": 0},
		"is_redacted": true
	}`)
	var s SidebarSection
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !s.IsRedacted {
		t.Errorf("IsRedacted = false, want true")
	}
	if s.ID != "L_R" || s.Name != "Hidden" {
		t.Errorf("got %+v", s)
	}
	if s.Next != "" {
		t.Errorf("Next = %q, want empty (null in JSON)", s.Next)
	}
}

func TestDecodeSection_WS(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "ws_section_upserted.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var ev wsChannelSectionUpserted
	if err := json.Unmarshal(data, &ev); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	got := ev.toUpserted()
	if got.ID != "L0B12LBBCTD" {
		t.Errorf("ID = %q", got.ID)
	}
	if got.Name != "test2" {
		t.Errorf("Name = %q", got.Name)
	}
	if got.Type != "standard" {
		t.Errorf("Type = %q (WS uses channel_section_type, must normalize)", got.Type)
	}
	if got.Next != "L08BCNXM15Y" {
		t.Errorf("Next = %q", got.Next)
	}
	if got.LastUpdate != 1777720183 {
		t.Errorf("LastUpdate = %d (WS uses last_update, must normalize)", got.LastUpdate)
	}
}
