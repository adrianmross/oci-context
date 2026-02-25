package cmd

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/adrianmross/oci-context/pkg/config"
	"github.com/adrianmross/oci-context/pkg/oci"
	"github.com/adrianmross/oci-context/pkg/ocicfg"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
)

// resetTenancyCache clears the global tenancy name cache for tests.
func resetTenancyCache() {
	tenancyNamesMu.Lock()
	defer tenancyNamesMu.Unlock()
	tenancyNames = make(map[string]string)
}

func newTestContextItem() contextItem {
	return contextItem{config.Context{
		Name:            "dev",
		Profile:         "DEFAULT",
		TenancyOCID:     "ocid1.tenancy.oc1..ten",
		CompartmentOCID: "",
		Region:          "us-phoenix-1",
		User:            "ocid1.user.oc1..user",
	}}
}

func TestTUIQuitSavesOnQ(t *testing.T) {
	ci := newTestContextItem()
	cfg := config.Config{
		Options:  config.Options{OCIConfigPath: "/tmp/oci"},
		Contexts: []config.Context{ci.Context},
	}
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.yml")
    m := newTuiModel(cfg, cfgPath, []list.Item{ci}, nil, "")

	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	res := model.(tuiModel)

	if !res.finalized {
		t.Fatalf("expected finalized after q, got false")
	}
	if res.selected != "dev" {
		t.Fatalf("expected selected dev, got %s", res.selected)
	}
	if res.parentID != "ocid1.tenancy.oc1..ten" {
		t.Fatalf("expected parentID set to tenancy, got %s", res.parentID)
	}
}

func TestTUIEscQuitsWithoutSave(t *testing.T) {
	ci := newTestContextItem()
	cfg := config.Config{
		Options:  config.Options{OCIConfigPath: "/tmp/oci"},
		Contexts: []config.Context{ci.Context},
	}
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.yml")
    m := newTuiModel(cfg, cfgPath, []list.Item{ci}, nil, "")

	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	res := model.(tuiModel)

	if res.finalized {
		t.Fatalf("expected not finalized after esc")
	}
	if res.selected != "" {
		t.Fatalf("expected no selected context after esc, got %s", res.selected)
	}
}

func TestTUIFilteringGuardsHotkeys(t *testing.T) {
	ci := newTestContextItem()
	cfg := config.Config{
		Options:  config.Options{OCIConfigPath: "/tmp/oci"},
		Contexts: []config.Context{ci.Context},
	}
    m := newTuiModel(cfg, "", []list.Item{ci}, nil, "")
	// put contexts list into filtering mode
	m.list.SetFilteringEnabled(true)
	m.list.SetFilterState(list.Filtering)

	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	res := model.(tuiModel)

	if res.mode != "contexts" {
		t.Fatalf("expected to remain in contexts mode while filtering, got %s", res.mode)
	}
	if res.list.FilterState() != list.Filtering {
		t.Fatalf("expected filtering state to remain active")
	}
}

func TestTUIEnterAppliesFilterAndExits(t *testing.T) {
	ci := newTestContextItem()
	cfg := config.Config{
		Options:  config.Options{OCIConfigPath: "/tmp/oci"},
		Contexts: []config.Context{ci.Context},
	}
    m := newTuiModel(cfg, "", []list.Item{ci}, nil, "")
	m.list.SetFilteringEnabled(true)
	m.list.SetFilterState(list.Filtering)

	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	res := model.(tuiModel)

	if res.list.FilterState() == list.Filtering {
		t.Fatalf("expected filtering to end after enter")
	}
	if res.list.Index() != 0 {
		t.Fatalf("expected selection index 0 after enter, got %d", res.list.Index())
	}
}

func TestTUISpaceStagesCompartment(t *testing.T) {
	ci := newTestContextItem()
	cfg := config.Config{
		Options:  config.Options{OCIConfigPath: "/tmp/oci"},
		Contexts: []config.Context{ci.Context},
	}
    m := newTuiModel(cfg, "", []list.Item{ci}, nil, "")
	// move to compartments mode with one item selected
	m.mode = "compartments"
	comp := compItem{oc: oci.Compartment{ID: "ocid1.compartment.oc1..child", Name: "child", Parent: ci.TenancyOCID, Status: "ACTIVE"}}
	m.comps.SetItems([]list.Item{comp})
	m.comps.Select(0)

	model, _ := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	res := model.(tuiModel)

	if res.pendingSelectionID != "ocid1.compartment.oc1..child" {
		t.Fatalf("expected pendingSelectionID set, got %s", res.pendingSelectionID)
	}
	if res.parentID != "ocid1.compartment.oc1..child" {
		t.Fatalf("expected parentID updated to child, got %s", res.parentID)
	}
}

func TestTUISpaceStagesRegion(t *testing.T) {
	ci := newTestContextItem()
	cfg := config.Config{
		Options:  config.Options{OCIConfigPath: "/tmp/oci"},
		Contexts: []config.Context{ci.Context},
	}
    m := newTuiModel(cfg, "", []list.Item{ci}, nil, "")
	m.mode = "regions"
	m.regions.SetItems(toRegionList([]string{"us-phoenix-1", "us-ashburn-1"}))
	m.regions.Select(1) // select us-ashburn-1

	model, _ := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	res := model.(tuiModel)

	if res.pendingRegion != "us-ashburn-1" {
		t.Fatalf("expected pendingRegion set, got %s", res.pendingRegion)
	}
	if res.ctxItem.Region != "us-ashburn-1" {
		t.Fatalf("expected ctxItem.Region updated, got %s", res.ctxItem.Region)
	}
}

func TestPrimeTenancyNamesCachesFriendlyNames(t *testing.T) {
	resetTenancyCache()
	orig := fetchIdentityDetails
	defer func() { fetchIdentityDetails = orig }()

	fetchIdentityDetails = func(ctx context.Context, cfgPath, profile, region, tenancyOCID, compartmentOCID, userOCID string) (oci.IdentityDetails, error) {
		return oci.IdentityDetails{TenancyName: "My Tenancy", TenancyOCID: tenancyOCID}, nil
	}

	profiles := map[string]ocicfg.Profile{
		"DEFAULT": {Tenancy: "ocid1.tenancy.oc1..xyz", Region: "us-phoenix-1", User: "ocid1.user.oc1..user"},
	}
	primeTenancyNames(context.Background(), profiles, "/tmp/oci")

	if got := lookupTenancyName("ocid1.tenancy.oc1..xyz"); got != "My Tenancy" {
		t.Fatalf("expected cached tenancy name, got %q", got)
	}

	items := tenanciesFromProfiles(profiles)
	if len(items) != 1 {
		t.Fatalf("expected 1 tenancy item, got %d", len(items))
	}
	if ti, ok := items[0].(tenancyItem); ok {
		if ti.Title() != "My Tenancy" {
			t.Fatalf("expected title to use friendly name, got %q", ti.Title())
		}
	} else {
		t.Fatalf("expected tenancyItem type")
	}
}
