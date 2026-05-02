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
