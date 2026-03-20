package cmd

import (
	"context"
	"path/filepath"
	"strings"
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
	return contextItem{Context: config.Context{
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
	m.list.SetFilterText("dev")
	m.list.SetFilterState(list.Filtering)

	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	res := model.(tuiModel)

	if res.list.FilterState() != list.FilterApplied {
		t.Fatalf("expected filter to be applied after enter, got state=%v", res.list.FilterState())
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
}

func TestTUISpaceStagesContext(t *testing.T) {
	ci := newTestContextItem()
	cfg := config.Config{
		Options:  config.Options{OCIConfigPath: "/tmp/oci"},
		Contexts: []config.Context{ci.Context},
	}
	m := newTuiModel(cfg, "", []list.Item{ci}, nil, "")
	m.mode = "contexts"
	m.list.Select(0)

	model, _ := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	res := model.(tuiModel)

	if res.pendingContextName != "dev" {
		t.Fatalf("expected pendingContextName set, got %s", res.pendingContextName)
	}

	// Move cursor and ensure staged state persists independently of navigation.
	model, _ = res.Update(tea.KeyMsg{Type: tea.KeyDown})
	res = model.(tuiModel)
	if res.pendingContextName != "dev" {
		t.Fatalf("expected pendingContextName to persist after navigation, got %s", res.pendingContextName)
	}
}

func TestTUISpaceTogglesUnstageContext(t *testing.T) {
	ci := newTestContextItem()
	cfg := config.Config{
		Options:  config.Options{OCIConfigPath: "/tmp/oci"},
		Contexts: []config.Context{ci.Context},
	}
	m := newTuiModel(cfg, "", []list.Item{ci}, nil, "")
	m.mode = "contexts"
	m.list.Select(0)

	model, _ := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	res := model.(tuiModel)
	if res.pendingContextName != "dev" {
		t.Fatalf("expected pending context set, got %s", res.pendingContextName)
	}

	model, _ = res.Update(tea.KeyMsg{Type: tea.KeySpace})
	res = model.(tuiModel)
	if res.pendingContextName != "" {
		t.Fatalf("expected pending context cleared on second space, got %s", res.pendingContextName)
	}
}

func TestTUIViewShowsCompactMetaLine(t *testing.T) {
	ci := newTestContextItem()
	cfg := config.Config{
		Options:  config.Options{OCIConfigPath: "/tmp/oci"},
		Contexts: []config.Context{ci.Context},
	}
	m := newTuiModel(cfg, "", []list.Item{ci}, nil, "")
	m.mode = "contexts"
	m.ctxItem = ci
	m.pendingContextName = ci.Name

	view := m.View()
	if !strings.Contains(view, "mode:contexts") {
		t.Fatalf("expected compact meta line to include mode, got: %s", view)
	}
	if !strings.Contains(view, "staged:ctx:dev") {
		t.Fatalf("expected compact meta line to include staged context, got: %s", view)
	}
	if !strings.Contains(view, "STAGED") {
		t.Fatalf("expected staged row marker in list output, got: %s", view)
	}
}

func TestTUIDensityModeShowsDescriptionsOnlyWhenNotUltra(t *testing.T) {
	ci := newTestContextItem()
	cfg := config.Config{
		Options:  config.Options{OCIConfigPath: "/tmp/oci"},
		Contexts: []config.Context{ci.Context},
	}
	m := newTuiModel(cfg, "", []list.Item{ci}, nil, "")
	m.mode = "contexts"

	normalView := m.View()
	if !strings.Contains(normalView, "profile=DEFAULT region=us-phoenix-1") {
		t.Fatalf("expected normal mode to show row description, got: %s", normalView)
	}

	m.ultraCompact = true
	m.refreshDelegates()
	ultraView := m.View()
	if strings.Contains(ultraView, "profile=DEFAULT region=us-phoenix-1") {
		t.Fatalf("expected ultra mode to hide row description, got: %s", ultraView)
	}
}

func TestTUIEscDoesNotSaveAfterStaging(t *testing.T) {
	ci := newTestContextItem()
	cfg := config.Config{
		Options:  config.Options{OCIConfigPath: "/tmp/oci"},
		Contexts: []config.Context{ci.Context},
	}
	m := newTuiModel(cfg, "", []list.Item{ci}, nil, "")
	m.mode = "regions"
	m.regions.SetItems(toRegionList([]string{"us-phoenix-1", "us-ashburn-1"}))
	m.regions.Select(1)

	model, _ := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	res := model.(tuiModel)
	if res.pendingRegion != "us-ashburn-1" {
		t.Fatalf("expected staged region before esc")
	}

	model, _ = res.Update(tea.KeyMsg{Type: tea.KeyEsc})
	res = model.(tuiModel)
	if res.finalized {
		t.Fatalf("expected esc to quit without save after staging")
	}
}

func TestTUIEscClearsAppliedFilterBeforeQuit(t *testing.T) {
	ci := newTestContextItem()
	cfg := config.Config{
		Options:  config.Options{OCIConfigPath: "/tmp/oci"},
		Contexts: []config.Context{ci.Context},
	}
	m := newTuiModel(cfg, "", []list.Item{ci}, nil, "")
	m.mode = "contexts"
	m.list.SetFilteringEnabled(true)
	m.list.SetFilterText("dev")
	m.list.SetFilterState(list.FilterApplied)

	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	res := model.(tuiModel)
	if res.list.FilterState() != list.Unfiltered {
		t.Fatalf("expected first esc to clear applied filter, got state=%v", res.list.FilterState())
	}
	if res.finalized {
		t.Fatalf("expected not finalized when clearing filter")
	}
}

func TestTUIQAndCtrlSSaveEquivalentInRegions(t *testing.T) {
	base := newTestContextItem()
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.yml")
	newModel := func() tuiModel {
		cfg := config.Config{
			Options:  config.Options{OCIConfigPath: "/tmp/oci"},
			Contexts: []config.Context{base.Context},
		}
		m := newTuiModel(cfg, cfgPath, []list.Item{base}, nil, "")
		m.mode = "regions"
		m.ctxItem = base
		m.parentID = base.TenancyOCID
		m.regions.SetItems(toRegionList([]string{"us-phoenix-1", "us-ashburn-1"}))
		m.regions.Select(1)
		return m
	}

	qModel, _ := newModel().Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	qRes := qModel.(tuiModel)
	if !qRes.finalized || qRes.ctxItem.Region != "us-ashburn-1" {
		t.Fatalf("expected q to save selected region")
	}

	sModel, _ := newModel().Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	sRes := sModel.(tuiModel)
	if !sRes.finalized || sRes.ctxItem.Region != "us-ashburn-1" {
		t.Fatalf("expected ctrl+s to save selected region")
	}
}

func TestTUICompartmentStagePersistsAcrossMenuSwitch(t *testing.T) {
	ctxA := contextItem{Context: config.Context{Name: "DEFAULT", Profile: "DEFAULT", TenancyOCID: "ocid1.tenancy.oc1..ten", CompartmentOCID: "ocid1.tenancy.oc1..ten", Region: "us-phoenix-1"}}
	ctxB := contextItem{Context: config.Context{Name: "SECOND", Profile: "SECOND", TenancyOCID: "ocid1.tenancy.oc1..ten", CompartmentOCID: "ocid1.tenancy.oc1..ten", Region: "us-ashburn-1"}}
	cfg := config.Config{Options: config.Options{OCIConfigPath: "/tmp/oci"}, Contexts: []config.Context{ctxA.Context, ctxB.Context}}
	m := newTuiModel(cfg, "", []list.Item{ctxA, ctxB}, map[string]ocicfg.Profile{
		"DEFAULT": {Tenancy: ctxA.TenancyOCID, Region: ctxA.Region},
		"SECOND":  {Tenancy: ctxB.TenancyOCID, Region: ctxB.Region},
	}, "")

	m.mode = "compartments"
	m.ctxItem = ctxA
	staged := "ocid1.compartment.oc1..child"
	comp := compItem{oc: oci.Compartment{ID: staged, Name: "child", Parent: ctxA.TenancyOCID, Status: "ACTIVE"}}
	m.comps.SetItems([]list.Item{comp})
	m.comps.Select(0)

	model, _ := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	res := model.(tuiModel)
	if res.pendingSelectionID != staged {
		t.Fatalf("expected staged compartment")
	}

	model, _ = res.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'T'}})
	res = model.(tuiModel)
	if res.mode != "tenancies" {
		t.Fatalf("expected tenancies mode")
	}

	model, _ = res.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'P'}})
	res = model.(tuiModel)
	if res.mode != "contexts" {
		t.Fatalf("expected contexts mode")
	}

	if res.pendingSelectionID != staged {
		t.Fatalf("expected staged compartment to persist across menus, got %s", res.pendingSelectionID)
	}
}

func TestTUIQFromContextsUsesStagedCompartmentSelection(t *testing.T) {
	ctx := contextItem{Context: config.Context{Name: "DEFAULT", Profile: "DEFAULT", TenancyOCID: "ocid1.tenancy.oc1..ten", CompartmentOCID: "ocid1.tenancy.oc1..ten", Region: "us-phoenix-1"}}
	cfg := config.Config{Options: config.Options{OCIConfigPath: "/tmp/oci"}, Contexts: []config.Context{ctx.Context}}
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.yml")
	m := newTuiModel(cfg, cfgPath, []list.Item{ctx}, map[string]ocicfg.Profile{
		"DEFAULT": {Tenancy: ctx.TenancyOCID, Region: ctx.Region},
	}, "")

	// Stage a non-root compartment.
	m.mode = "compartments"
	m.ctxItem = ctx
	staged := "ocid1.compartment.oc1..wiz"
	m.parentID = staged
	m.pendingSelectionID = staged

	// Return to profiles and quit+save.
	m.mode = "contexts"
	m.list.Select(0)
	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	res := model.(tuiModel)

	if !res.finalized {
		t.Fatalf("expected q to finalize")
	}
	if res.parentID != staged {
		t.Fatalf("expected staged compartment to be saved, got %s", res.parentID)
	}
	if got := res.ctxItem.CompartmentOCID; got != staged {
		t.Fatalf("expected ctxItem compartment %s, got %s", staged, got)
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

func TestTUISpaceTogglesUnstageCompartment(t *testing.T) {
	ci := newTestContextItem()
	cfg := config.Config{
		Options:  config.Options{OCIConfigPath: "/tmp/oci"},
		Contexts: []config.Context{ci.Context},
	}
	m := newTuiModel(cfg, "", []list.Item{ci}, nil, "")
	m.mode = "compartments"
	comp := compItem{oc: oci.Compartment{ID: "ocid1.compartment.oc1..child", Name: "child", Parent: ci.TenancyOCID, Status: "ACTIVE"}}
	m.comps.SetItems([]list.Item{comp})
	m.comps.Select(0)

	model, _ := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	res := model.(tuiModel)
	if res.pendingSelectionID == "" {
		t.Fatalf("expected pending compartment set")
	}

	model, _ = res.Update(tea.KeyMsg{Type: tea.KeySpace})
	res = model.(tuiModel)
	if res.pendingSelectionID != "" {
		t.Fatalf("expected pending compartment cleared on second space, got %s", res.pendingSelectionID)
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

func TestProfileMenuItemsHidesContextDuplicatesOfProfiles(t *testing.T) {
	profiles := map[string]ocicfg.Profile{
		"DEFAULT": {
			Tenancy: "ocid1.tenancy.oc1..ten",
			Region:  "us-phoenix-1",
			User:    "ocid1.user.oc1..u",
		},
	}
	cfg := config.Config{
		Options: config.Options{OCIConfigPath: "/tmp/oci"},
		Contexts: []config.Context{
			{
				Name:            "DEFAULT",
				Profile:         "DEFAULT",
				TenancyOCID:     "ocid1.tenancy.oc1..ten",
				CompartmentOCID: "ocid1.tenancy.oc1..ten",
				Region:          "us-phoenix-1",
			},
			{
				Name:            "DEFAULT@us-ashburn-1",
				Profile:         "DEFAULT",
				TenancyOCID:     "ocid1.tenancy.oc1..ten",
				CompartmentOCID: "ocid1.tenancy.oc1..ten",
				Region:          "us-ashburn-1",
			},
		},
	}

	items := profileMenuItems(cfg, profiles, nil)
	var titles []string
	for _, it := range items {
		titles = append(titles, itemTitle(it))
	}
	got := strings.Join(titles, " | ")
	if strings.Contains(got, "CONTEXTS | DEFAULT |") {
		t.Fatalf("expected duplicate DEFAULT context to be hidden, got %q", got)
	}
	if !strings.Contains(got, "DEFAULT@us-ashburn-1") {
		t.Fatalf("expected differing context to remain visible, got %q", got)
	}
}

func TestProfileMenuItemsHidesLegacyEquivalentContexts(t *testing.T) {
	profiles := map[string]ocicfg.Profile{
		"DEFAULT": {
			Tenancy: "ocid1.tenancy.oc1..ten",
			Region:  "us-phoenix-1",
			User:    "ocid1.user.oc1..u",
		},
	}
	cfg := config.Config{
		Options: config.Options{OCIConfigPath: "/tmp/oci"},
		Contexts: []config.Context{
			{
				Name:            "DEFAULT",
				Profile:         "", // legacy missing profile field
				TenancyOCID:     "", // legacy missing tenancy
				CompartmentOCID: "", // legacy implicit root
				Region:          "", // legacy missing region
			},
		},
	}

	items := profileMenuItems(cfg, profiles, nil)
	var titles []string
	for _, it := range items {
		titles = append(titles, itemTitle(it))
	}
	got := strings.Join(titles, " | ")
	if strings.Contains(got, "CONTEXTS") {
		t.Fatalf("expected legacy equivalent context to be hidden, got %q", got)
	}
}

func TestProfileMenuItemsShowsContextsFirstAndCurrentFirst(t *testing.T) {
	profiles := map[string]ocicfg.Profile{
		"DEFAULT": {Tenancy: "ocid1.tenancy.oc1..ten", Region: "us-phoenix-1"},
	}
	cfg := config.Config{
		Options:        config.Options{OCIConfigPath: "/tmp/oci"},
		CurrentContext: "B",
		Contexts: []config.Context{
			{Name: "A", Profile: "DEFAULT", TenancyOCID: "ocid1.tenancy.oc1..ten", CompartmentOCID: "ocid1.compartment.oc1..a", Region: "us-phoenix-1"},
			{Name: "B", Profile: "DEFAULT", TenancyOCID: "ocid1.tenancy.oc1..ten", CompartmentOCID: "ocid1.compartment.oc1..b", Region: "us-phoenix-1"},
		},
	}

	items := profileMenuItems(cfg, profiles, nil)
	var firstContext contextItem
	found := false
	for _, it := range items {
		if ci, ok := it.(contextItem); ok && ci.fromSaved {
			firstContext = ci
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected at least one saved context in menu")
	}
	if firstContext.Name != "B" {
		t.Fatalf("expected current context first, got %s", firstContext.Name)
	}
}

func TestProfileMenuItemsDedupesEquivalentSavedContextsKeepingCurrent(t *testing.T) {
	profiles := map[string]ocicfg.Profile{
		"DEFAULT": {Tenancy: "ocid1.tenancy.oc1..ten", Region: "us-phoenix-1"},
	}
	cfg := config.Config{
		Options:        config.Options{OCIConfigPath: "/tmp/oci"},
		CurrentContext: "DEFAULT@us-phoenix-1/ocid1.…bbbbbb",
		Contexts: []config.Context{
			{
				Name:            "DEFAULT",
				Profile:         "DEFAULT",
				TenancyOCID:     "ocid1.tenancy.oc1..ten",
				CompartmentOCID: "ocid1.compartment.oc1..bbbbbb",
				Region:          "us-phoenix-1",
			},
			{
				Name:            "DEFAULT@us-phoenix-1/ocid1.…bbbbbb",
				Profile:         "DEFAULT",
				TenancyOCID:     "ocid1.tenancy.oc1..ten",
				CompartmentOCID: "ocid1.compartment.oc1..bbbbbb",
				Region:          "us-phoenix-1",
			},
		},
	}

	items := profileMenuItems(cfg, profiles, nil)
	var savedNames []string
	for _, it := range items {
		if ci, ok := it.(contextItem); ok && ci.fromSaved {
			savedNames = append(savedNames, ci.Name)
		}
	}
	if len(savedNames) != 1 {
		t.Fatalf("expected one deduped saved context, got %v", savedNames)
	}
	if savedNames[0] != cfg.CurrentContext {
		t.Fatalf("expected current context to be retained, got %v", savedNames)
	}
}

func TestProfileMenuItemsCurrentEquivalentContextPromotesToProfileCurrentLabel(t *testing.T) {
	profiles := map[string]ocicfg.Profile{
		"DEFAULT": {
			Tenancy: "ocid1.tenancy.oc1..ten",
			Region:  "us-phoenix-1",
		},
	}
	cfg := config.Config{
		Options:        config.Options{OCIConfigPath: "/tmp/oci"},
		CurrentContext: "DEFAULT",
		Contexts: []config.Context{
			{
				Name:            "DEFAULT",
				Profile:         "DEFAULT",
				TenancyOCID:     "ocid1.tenancy.oc1..ten",
				CompartmentOCID: "ocid1.tenancy.oc1..ten",
				Region:          "us-phoenix-1",
			},
		},
	}

	items := profileMenuItems(cfg, profiles, nil)
	var titles []string
	var hasSavedCurrent bool
	for _, it := range items {
		titles = append(titles, itemTitle(it))
		if ci, ok := it.(contextItem); ok && ci.fromSaved && ci.isCurrent {
			hasSavedCurrent = true
		}
	}
	got := strings.Join(titles, " | ")
	if hasSavedCurrent {
		t.Fatalf("expected equivalent saved current context to be deduped, got %q", got)
	}
	if !strings.Contains(got, "DEFAULT @CURRENT") {
		t.Fatalf("expected current profile row to be marked, got %q", got)
	}
}

func TestTUIContextNavigationSkipsSectionAndSeparatorRows(t *testing.T) {
	profiles := map[string]ocicfg.Profile{
		"DEFAULT": {Tenancy: "ocid1.tenancy.oc1..ten", Region: "us-phoenix-1"},
		"ALT":     {Tenancy: "ocid1.tenancy.oc1..ten", Region: "us-ashburn-1"},
	}
	cfg := config.Config{
		Options:        config.Options{OCIConfigPath: "/tmp/oci"},
		CurrentContext: "CTX",
		Contexts: []config.Context{
			{
				Name:            "CTX",
				Profile:         "DEFAULT",
				TenancyOCID:     "ocid1.tenancy.oc1..ten",
				CompartmentOCID: "ocid1.compartment.oc1..abc",
				Region:          "us-phoenix-1",
			},
		},
	}

	items := profileMenuItemsForDensity(cfg, profiles, nil, true)
	m := newTuiModel(cfg, "", items, profiles, "")
	m.mode = "contexts"

	// Select the saved context row, then one down should land on first profile row.
	for i, it := range m.list.Items() {
		if ci, ok := it.(contextItem); ok && ci.fromSaved {
			m.list.Select(i)
			break
		}
	}
	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	res := model.(tuiModel)
	selected, ok := res.list.SelectedItem().(contextItem)
	if !ok {
		t.Fatalf("expected context item selected after down, got %T", res.list.SelectedItem())
	}
	if selected.fromSaved {
		t.Fatalf("expected to skip separator/header into profile row, got saved context %s", selected.Name)
	}

	// One up should return to saved context row, skipping separator/header in reverse.
	model, _ = res.Update(tea.KeyMsg{Type: tea.KeyUp})
	res = model.(tuiModel)
	selected, ok = res.list.SelectedItem().(contextItem)
	if !ok {
		t.Fatalf("expected context item selected after up, got %T", res.list.SelectedItem())
	}
	if !selected.fromSaved {
		t.Fatalf("expected to skip separator/header back to saved context, got profile %s", selected.Name)
	}
}

func TestTUITabCyclesMenusForward(t *testing.T) {
	profiles := map[string]ocicfg.Profile{
		"DEFAULT": {Tenancy: "ocid1.tenancy.oc1..ten", Region: "us-phoenix-1"},
	}
	cfg := config.Config{
		Options:        config.Options{OCIConfigPath: "/tmp/oci"},
		CurrentContext: "DEFAULT",
		Contexts: []config.Context{
			{
				Name:            "DEFAULT",
				Profile:         "DEFAULT",
				TenancyOCID:     "ocid1.tenancy.oc1..ten",
				CompartmentOCID: "ocid1.tenancy.oc1..ten",
				Region:          "us-phoenix-1",
			},
		},
	}
	m := newTuiModel(cfg, "", profileMenuItems(cfg, profiles, nil), profiles, "")

	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	res := model.(tuiModel)
	if res.mode != "tenancies" {
		t.Fatalf("expected tab from contexts to go to tenancies, got %s", res.mode)
	}

	model, _ = res.Update(tea.KeyMsg{Type: tea.KeyTab})
	res = model.(tuiModel)
	if res.mode != "compartments" {
		t.Fatalf("expected tab from tenancies to go to compartments, got %s", res.mode)
	}

	model, _ = res.Update(tea.KeyMsg{Type: tea.KeyTab})
	res = model.(tuiModel)
	if res.mode != "regions" {
		t.Fatalf("expected tab from compartments to go to regions, got %s", res.mode)
	}

	model, _ = res.Update(tea.KeyMsg{Type: tea.KeyTab})
	res = model.(tuiModel)
	if res.mode != "contexts" {
		t.Fatalf("expected tab from regions to go to contexts, got %s", res.mode)
	}
}

func TestTUIShiftTabCyclesMenusBackward(t *testing.T) {
	profiles := map[string]ocicfg.Profile{
		"DEFAULT": {Tenancy: "ocid1.tenancy.oc1..ten", Region: "us-phoenix-1"},
	}
	cfg := config.Config{
		Options:        config.Options{OCIConfigPath: "/tmp/oci"},
		CurrentContext: "DEFAULT",
		Contexts: []config.Context{
			{
				Name:            "DEFAULT",
				Profile:         "DEFAULT",
				TenancyOCID:     "ocid1.tenancy.oc1..ten",
				CompartmentOCID: "ocid1.tenancy.oc1..ten",
				Region:          "us-phoenix-1",
			},
		},
	}
	m := newTuiModel(cfg, "", profileMenuItems(cfg, profiles, nil), profiles, "")

	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	res := model.(tuiModel)
	if res.mode != "regions" {
		t.Fatalf("expected shift+tab from contexts to go to regions, got %s", res.mode)
	}

	model, _ = res.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	res = model.(tuiModel)
	if res.mode != "compartments" {
		t.Fatalf("expected shift+tab from regions to go to compartments, got %s", res.mode)
	}
}

func TestTUIFilterPlaceholderHintIsSet(t *testing.T) {
	ci := newTestContextItem()
	cfg := config.Config{
		Options:  config.Options{OCIConfigPath: "/tmp/oci"},
		Contexts: []config.Context{ci.Context},
	}
	m := newTuiModel(cfg, "", []list.Item{ci}, nil, "")

	if m.list.FilterInput.Placeholder != filterPlaceholderHint {
		t.Fatalf("expected contexts filter placeholder %q, got %q", filterPlaceholderHint, m.list.FilterInput.Placeholder)
	}
	if m.tenancies.FilterInput.Placeholder != filterPlaceholderHint {
		t.Fatalf("expected tenancies filter placeholder %q, got %q", filterPlaceholderHint, m.tenancies.FilterInput.Placeholder)
	}
	if m.comps.FilterInput.Placeholder != filterPlaceholderHint {
		t.Fatalf("expected compartments filter placeholder %q, got %q", filterPlaceholderHint, m.comps.FilterInput.Placeholder)
	}
	if m.regions.FilterInput.Placeholder != filterPlaceholderHint {
		t.Fatalf("expected regions filter placeholder %q, got %q", filterPlaceholderHint, m.regions.FilterInput.Placeholder)
	}
}

func TestWithCurrentMarkerAddsLabel(t *testing.T) {
	item := contextItem{Context: config.Context{Name: "DEFAULT"}}
	marked := withCurrentMarker(item, false)
	title := itemTitle(marked)
	if !strings.Contains(title, "CURRENT") {
		t.Fatalf("expected CURRENT marker in title, got %q", title)
	}
	compact := withCurrentMarker(item, true)
	if got := itemTitle(compact); !strings.HasPrefix(got, "[=] ") {
		t.Fatalf("expected compact current prefix, got %q", got)
	}
}

func TestTUIInitializesSavedSelectionFromCurrentContext(t *testing.T) {
	ci := contextItem{Context: config.Context{
		Name:            "dev",
		Profile:         "DEFAULT",
		TenancyOCID:     "ocid1.tenancy.oc1..ten",
		CompartmentOCID: "ocid1.compartment.oc1..abc",
		Region:          "us-phoenix-1",
	}}
	cfg := config.Config{
		Options:        config.Options{OCIConfigPath: "/tmp/oci"},
		CurrentContext: "dev",
		Contexts:       []config.Context{ci.Context},
	}
	m := newTuiModel(cfg, "", []list.Item{ci}, nil, "")

	if m.savedContextName != "dev" {
		t.Fatalf("expected saved context name dev, got %q", m.savedContextName)
	}
	if m.savedTenancyOCID != ci.TenancyOCID {
		t.Fatalf("expected saved tenancy %q, got %q", ci.TenancyOCID, m.savedTenancyOCID)
	}
	if m.savedCompartmentID != ci.CompartmentOCID {
		t.Fatalf("expected saved compartment %q, got %q", ci.CompartmentOCID, m.savedCompartmentID)
	}
	if m.savedRegion != ci.Region {
		t.Fatalf("expected saved region %q, got %q", ci.Region, m.savedRegion)
	}
}

func TestTUIStagingCompartmentAutoStagesTenancy(t *testing.T) {
	ci := newTestContextItem()
	cfg := config.Config{
		Options:  config.Options{OCIConfigPath: "/tmp/oci"},
		Contexts: []config.Context{ci.Context},
	}
	m := newTuiModel(cfg, "", []list.Item{ci}, nil, "")
	m.mode = "compartments"
	m.ctxItem = ci
	comp := compItem{oc: oci.Compartment{ID: "ocid1.compartment.oc1..child", Name: "child", Parent: ci.TenancyOCID, Status: "ACTIVE"}}
	m.comps.SetItems([]list.Item{comp})
	m.comps.Select(0)

	model, _ := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	res := model.(tuiModel)
	if res.pendingSelectionID != "ocid1.compartment.oc1..child" {
		t.Fatalf("expected staged compartment, got %q", res.pendingSelectionID)
	}
	if res.pendingTenancyOCID != ci.TenancyOCID {
		t.Fatalf("expected auto-staged tenancy %q, got %q", ci.TenancyOCID, res.pendingTenancyOCID)
	}
	if !res.autoStagedTenancy {
		t.Fatalf("expected autoStagedTenancy=true")
	}

	model, _ = res.Update(tea.KeyMsg{Type: tea.KeySpace})
	res = model.(tuiModel)
	if res.pendingSelectionID != "" {
		t.Fatalf("expected compartment to unstage")
	}
	if res.pendingTenancyOCID != "" {
		t.Fatalf("expected auto-staged tenancy to clear when compartment unstaged, got %q", res.pendingTenancyOCID)
	}
}

func TestTUIRenderTabsShowsStagedDotPerMenu(t *testing.T) {
	ci := newTestContextItem()
	cfg := config.Config{
		Options:  config.Options{OCIConfigPath: "/tmp/oci"},
		Contexts: []config.Context{ci.Context},
	}
	m := newTuiModel(cfg, "", []list.Item{ci}, nil, "")
	m.pendingRegion = "us-ashburn-1"
	m.pendingSelectionID = "ocid1.compartment.oc1..child"

	tabs := m.renderTabs()
	if !strings.Contains(tabs, "Reg") && !strings.Contains(tabs, "Regions") {
		t.Fatalf("expected regions tab to render, got %q", tabs)
	}
	if !strings.Contains(tabs, "Comp") && !strings.Contains(tabs, "Compartments") {
		t.Fatalf("expected compartments tab to render, got %q", tabs)
	}
	if !strings.Contains(tabs, "●") {
		t.Fatalf("expected staged dot in tab bar, got %q", tabs)
	}
}

func TestTUIEnterDrillsWithMarkedCompItem(t *testing.T) {
	ci := newTestContextItem()
	cfg := config.Config{
		Options:  config.Options{OCIConfigPath: "/tmp/oci"},
		Contexts: []config.Context{ci.Context},
	}
	m := newTuiModel(cfg, "", []list.Item{ci}, nil, "")
	m.mode = "compartments"
	m.ctxItem = ci
	child := compItem{oc: oci.Compartment{ID: "ocid1.compartment.oc1..child", Name: "child", Parent: ci.TenancyOCID, Status: "ACTIVE"}}
	m.comps.SetItems([]list.Item{markedItem{base: child, title: child.Title(), description: child.Description()}})
	m.comps.Select(0)

	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	res := model.(tuiModel)
	if res.parentID != child.oc.ID {
		t.Fatalf("expected to drill to child compartment %q, got %q", child.oc.ID, res.parentID)
	}
}
