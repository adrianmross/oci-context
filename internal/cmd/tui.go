package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/term"

	"github.com/adrianmross/oci-context/pkg/config"
	"github.com/adrianmross/oci-context/pkg/oci"
	"github.com/adrianmross/oci-context/pkg/ocicfg"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

var (
	stagedColor = lipgloss.Color("205")
	infoColor   = lipgloss.Color("244")
)

var (
	tenancyNames         = make(map[string]string)
	tenancyNamesMu       sync.RWMutex
	fetchIdentityDetails = oci.FetchIdentityDetails
)

// primeTenancyNames fetches friendly tenancy names for the given profiles and caches them.
// It runs best-effort: errors are ignored and missing names fall back to profile/OCID display.
func primeTenancyNames(ctx context.Context, profiles map[string]ocicfg.Profile, ociCfgPath string) {
	if len(profiles) == 0 || ociCfgPath == "" {
		return
	}
	// avoid refetching already cached tenancies
	needed := make(map[string]ocicfg.Profile) // tenancyOCID -> representative profile (with region)
	for _, p := range profiles {
		if lookupTenancyName(p.Tenancy) != "" {
			continue
		}
		needed[p.Tenancy] = p
	}
	if len(needed) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4) // limit concurrency to 4
	for tenancyOCID, profile := range needed {
		wg.Add(1)
		go func(tid string, prof ocicfg.Profile) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			// Use a profile name that belongs to this tenancy so identity calls can resolve the tenancy name.
			// We don't care which profile, only that its tenancy/region are valid. Since we lost the profile
			// name when grouping by tenancy above, reuse the tenancy OCID as a stand-in profile name when
			// making the identity call; the SDK only needs credentials from the config file, which is keyed
			// by profile name. To ensure we actually use a real profile name, select one from the original map
			// that matches the tenancy.
			profileName := ""
			for name, p := range profiles {
				if p.Tenancy == tid {
					profileName = name
					break
				}
			}
			if profileName == "" {
				return
			}
			details, err := fetchIdentityDetails(ctx, ociCfgPath, profileName, prof.Region, tid, "", "")
			if err != nil {
				return
			}
			recordTenancyName(tid, details.TenancyName)
		}(tenancyOCID, profile)
	}
	wg.Wait()
}

// lookupTenancyName returns a cached friendly name for the tenancy OCID.
func lookupTenancyName(ocid string) string {
	tenancyNamesMu.RLock()
	defer tenancyNamesMu.RUnlock()
	return tenancyNames[ocid]
}

// recordTenancyName stores a friendly name for the tenancy OCID.
func recordTenancyName(ocid, name string) {
	if ocid == "" || name == "" {
		return
	}
	tenancyNamesMu.Lock()
	defer tenancyNamesMu.Unlock()
	tenancyNames[ocid] = name
}

var fallbackRegions = []string{
	"us-ashburn-1",
	"us-phoenix-1",
	"eu-frankfurt-1",
	"eu-zurich-1",
	"uk-london-1",
	"ap-tokyo-1",
	"ap-osaka-1",
	"ap-seoul-1",
	"ap-mumbai-1",
	"ap-sydney-1",
	"ap-melbourne-1",
	"sa-saopaulo-1",
	"ca-toronto-1",
	"af-johannesburg-1",
	"me-dubai-1",
	"me-jeddah-1",
	"il-jerusalem-1",
}

// abbreviateOCID shortens an OCID for display.
func abbreviateOCID(s string) string {
	if len(s) <= 16 {
		return s
	}
	return fmt.Sprintf("%s…%s", s[:6], s[len(s)-6:])
}

func newTuiCmd() *cobra.Command {
	var cfgPath string
	var useGlobal bool
	cmd := &cobra.Command{
		Use:   "tui [mode]",
		Short: "Interactive context picker with compartment selection",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			useGlobal, err := cmd.Flags().GetBool("global")
			if err != nil {
				return err
			}
			path, err := resolveConfigPath(cfgPath, useGlobal)
			if err != nil {
				return err
			}
			cfg, err := config.Load(path)
			if err != nil {
				return err
			}
			profiles, perr := ocicfg.LoadProfiles(cfg.Options.OCIConfigPath)
			items := contextsFromProfiles(profiles)
			if perr != nil || len(items) == 0 {
				// fallback to stored contexts if profile parsing fails or yields none
				items = make([]list.Item, 0, len(cfg.Contexts))
				for _, ctx := range cfg.Contexts {
					items = append(items, contextItem{ctx})
				}
			}
			startMode := ""
			if len(args) == 1 {
				startMode = args[0]
			}
			m := newTuiModel(cfg, path, items, profiles, startMode)
			p := tea.NewProgram(m)
			finalModel, err := p.Run()
			if err != nil {
				return err
			}
			fm := finalModel.(tuiModel)
			if fm.selected != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "Switched to context %s\n", fm.selected)
			}
			return fm.err
		},
	}
	cmd.Flags().StringVarP(&cfgPath, "config", "c", "", "Path to config file")
	cmd.Flags().BoolVarP(&useGlobal, "global", "g", false, "Use global config (~/.oci-context/config.yml)")
	return cmd
}

// toRegionList converts region strings into list.Items.
func toRegionList(regions []string) []list.Item {
	items := make([]list.Item, 0, len(regions))
	for _, r := range regions {
		items = append(items, regionItem{name: r})
	}
	return items
}

// compDelegate wraps the default delegate to color the pending selection when present.
type compDelegate struct {
	list.DefaultDelegate
	pendingID *string
}

type markedItem struct {
	base  list.Item
	title string
}

func (m markedItem) Title() string       { return m.title }
func (m markedItem) Description() string { return "" }
func (m markedItem) FilterValue() string { return m.base.FilterValue() }

func withStageMarker(item list.Item) list.Item {
	return markedItem{base: item, title: "[*] " + itemTitle(item) + " [staged]"}
}

func itemTitle(item list.Item) string {
	switch it := item.(type) {
	case contextItem:
		return it.Title()
	case tenancyItem:
		return it.Title()
	case compItem:
		return it.Title()
	case regionItem:
		return it.Title()
	default:
		return item.FilterValue()
	}
}

func configureDefaultDelegateDensity(d *list.DefaultDelegate, ultraCompact bool) {
	if ultraCompact {
		d.SetHeight(1)
		d.SetSpacing(0)
		d.ShowDescription = false
		return
	}
	// In normal density, keep row descriptions visible (title + description).
	d.SetHeight(2)
	d.SetSpacing(0)
	d.ShowDescription = true
}

func newCompDelegate(pendingID *string, ultraCompact bool) *compDelegate {
	d := list.NewDefaultDelegate()
	configureDefaultDelegateDensity(&d, ultraCompact)
	return &compDelegate{DefaultDelegate: d, pendingID: pendingID}
}

func (d *compDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	if ci, ok := listItem.(compItem); ok && d.pendingID != nil && *d.pendingID != "" && ci.oc.ID == *d.pendingID {
		origNormalTitle := d.Styles.NormalTitle
		origNormalDesc := d.Styles.NormalDesc
		origTitle := d.Styles.SelectedTitle
		origDesc := d.Styles.SelectedDesc
		pendingTitle := origTitle.Foreground(stagedColor).Bold(true)
		pendingDesc := origDesc.Foreground(stagedColor).Bold(true)
		d.Styles.NormalTitle = pendingTitle
		d.Styles.NormalDesc = pendingDesc
		d.Styles.SelectedTitle = pendingTitle
		d.Styles.SelectedDesc = pendingDesc
		d.DefaultDelegate.Render(w, m, index, withStageMarker(listItem))
		d.Styles.NormalTitle = origNormalTitle
		d.Styles.NormalDesc = origNormalDesc
		d.Styles.SelectedTitle = origTitle
		d.Styles.SelectedDesc = origDesc
		return
	}
	d.DefaultDelegate.Render(w, m, index, listItem)
}

// regionDelegate highlights pending region selection when present.
type regionDelegate struct {
	list.DefaultDelegate
	pendingName *string
}

// contextDelegate highlights pending context selection when present.
type contextDelegate struct {
	list.DefaultDelegate
	pendingName *string
}

func newContextDelegate(pendingName *string, ultraCompact bool) *contextDelegate {
	d := list.NewDefaultDelegate()
	configureDefaultDelegateDensity(&d, ultraCompact)
	return &contextDelegate{DefaultDelegate: d, pendingName: pendingName}
}

func (d *contextDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	if ci, ok := listItem.(contextItem); ok && d.pendingName != nil && *d.pendingName != "" && ci.Name == *d.pendingName {
		origTitle := d.Styles.NormalTitle
		origDesc := d.Styles.NormalDesc
		origSelectedTitle := d.Styles.SelectedTitle
		origSelectedDesc := d.Styles.SelectedDesc
		pendingTitle := origTitle.Foreground(stagedColor).Bold(true)
		pendingDesc := origDesc.Foreground(stagedColor).Bold(true)
		d.Styles.NormalTitle = pendingTitle
		d.Styles.NormalDesc = pendingDesc
		d.Styles.SelectedTitle = pendingTitle
		d.Styles.SelectedDesc = pendingDesc
		d.DefaultDelegate.Render(w, m, index, withStageMarker(listItem))
		d.Styles.NormalTitle = origTitle
		d.Styles.NormalDesc = origDesc
		d.Styles.SelectedTitle = origSelectedTitle
		d.Styles.SelectedDesc = origSelectedDesc
		return
	}
	d.DefaultDelegate.Render(w, m, index, listItem)
}

// tenancyDelegate highlights pending tenancy selection when present.
type tenancyDelegate struct {
	list.DefaultDelegate
	pendingOCID *string
}

func newTenancyDelegate(pendingOCID *string, ultraCompact bool) *tenancyDelegate {
	d := list.NewDefaultDelegate()
	configureDefaultDelegateDensity(&d, ultraCompact)
	return &tenancyDelegate{DefaultDelegate: d, pendingOCID: pendingOCID}
}

func (d *tenancyDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	if ti, ok := listItem.(tenancyItem); ok && d.pendingOCID != nil && *d.pendingOCID != "" && ti.TenancyOCID == *d.pendingOCID {
		origTitle := d.Styles.NormalTitle
		origDesc := d.Styles.NormalDesc
		origSelectedTitle := d.Styles.SelectedTitle
		origSelectedDesc := d.Styles.SelectedDesc
		pendingTitle := origTitle.Foreground(stagedColor).Bold(true)
		pendingDesc := origDesc.Foreground(stagedColor).Bold(true)
		d.Styles.NormalTitle = pendingTitle
		d.Styles.NormalDesc = pendingDesc
		d.Styles.SelectedTitle = pendingTitle
		d.Styles.SelectedDesc = pendingDesc
		d.DefaultDelegate.Render(w, m, index, withStageMarker(listItem))
		d.Styles.NormalTitle = origTitle
		d.Styles.NormalDesc = origDesc
		d.Styles.SelectedTitle = origSelectedTitle
		d.Styles.SelectedDesc = origSelectedDesc
		return
	}
	d.DefaultDelegate.Render(w, m, index, listItem)
}

func newRegionDelegate(pendingName *string, ultraCompact bool) *regionDelegate {
	d := list.NewDefaultDelegate()
	configureDefaultDelegateDensity(&d, ultraCompact)
	return &regionDelegate{DefaultDelegate: d, pendingName: pendingName}
}

func (d *regionDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	if ri, ok := listItem.(regionItem); ok && d.pendingName != nil && *d.pendingName != "" && ri.name == *d.pendingName {
		origNormalTitle := d.Styles.NormalTitle
		origNormalDesc := d.Styles.NormalDesc
		origTitle := d.Styles.SelectedTitle
		origDesc := d.Styles.SelectedDesc
		pendingTitle := origTitle.Foreground(stagedColor).Bold(true)
		pendingDesc := origDesc.Foreground(stagedColor).Bold(true)
		d.Styles.NormalTitle = pendingTitle
		d.Styles.NormalDesc = pendingDesc
		d.Styles.SelectedTitle = pendingTitle
		d.Styles.SelectedDesc = pendingDesc
		d.DefaultDelegate.Render(w, m, index, withStageMarker(listItem))
		d.Styles.NormalTitle = origNormalTitle
		d.Styles.NormalDesc = origNormalDesc
		d.Styles.SelectedTitle = origTitle
		d.Styles.SelectedDesc = origDesc
		return
	}
	d.DefaultDelegate.Render(w, m, index, listItem)
}

// contextsFromProfiles builds context items from OCI CLI profiles.
func contextsFromProfiles(profiles map[string]ocicfg.Profile) []list.Item {
	names := make([]string, 0, len(profiles))
	for name := range profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	items := make([]list.Item, 0, len(names))
	for _, name := range names {
		p := profiles[name]
		items = append(items, contextItem{config.Context{
			Name:            name,
			Profile:         name,
			TenancyOCID:     p.Tenancy,
			CompartmentOCID: p.Tenancy,
			Region:          p.Region,
			User:            p.User,
		}})
	}
	return items
}

// tenanciesFromProfiles groups profiles by tenancy OCID into tenancy items.
func tenanciesFromProfiles(profiles map[string]ocicfg.Profile) []list.Item {
	tenantProfiles := make(map[string][]string)
	for name, p := range profiles {
		tenantProfiles[p.Tenancy] = append(tenantProfiles[p.Tenancy], name)
	}
	// sort tenancy OCIDs for stable order
	ocids := make([]string, 0, len(tenantProfiles))
	for ocid := range tenantProfiles {
		ocids = append(ocids, ocid)
	}
	sort.Strings(ocids)
	items := make([]list.Item, 0, len(ocids))
	for _, ocid := range ocids {
		profilesForTenancy := tenantProfiles[ocid]
		sort.Strings(profilesForTenancy)
		friendly := lookupTenancyName(ocid)
		items = append(items, tenancyItem{TenancyOCID: ocid, Profiles: profilesForTenancy, Name: friendly})
	}
	return items
}

// selectProfileForTenancy chooses a profile for the tenancy, preferring the configured default when it matches.
func selectProfileForTenancy(item tenancyItem, profiles map[string]ocicfg.Profile, defaultProfile string) string {
	if defaultProfile != "" {
		if p, ok := profiles[defaultProfile]; ok && p.Tenancy == item.TenancyOCID {
			return defaultProfile
		}
	}
	if len(item.Profiles) > 0 {
		return item.Profiles[0]
	}
	return ""
}

// contextItemForProfile builds a contextItem from a profile entry.
func contextItemForProfile(name string, p ocicfg.Profile) contextItem {
	return contextItem{config.Context{
		Name:            name,
		Profile:         name,
		TenancyOCID:     p.Tenancy,
		CompartmentOCID: p.Tenancy,
		Region:          p.Region,
		User:            p.User,
	}}
}

// isTerminal checks if stdout is a TTY.
func isTerminal() bool {
	return term.IsTerminal(int(os.Stdout.Fd()))
}

// runPromptFallback provides a non-TTY prompt-based flow.
func runPromptFallback(cmd *cobra.Command, cfgPathFlag string) error {
	useGlobal, err := cmd.Flags().GetBool("global")
	if err != nil {
		return err
	}
	path, err := resolveConfigPath(cfgPathFlag, useGlobal)
	if err != nil {
		return err
	}
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}
	profiles, perr := ocicfg.LoadProfiles(cfg.Options.OCIConfigPath)
	items := contextsFromProfiles(profiles)
	if perr != nil || len(items) == 0 {
		return fmt.Errorf("no profiles available from %s", cfg.Options.OCIConfigPath)
	}

	fmt.Fprintln(cmd.OutOrStdout(), "Select profile:")
	for i, it := range items {
		ci := it.(contextItem)
		fmt.Fprintf(cmd.OutOrStdout(), "%d) %s (tenancy=%s region=%s)\n", i+1, ci.Name, ci.TenancyOCID, ci.Region)
	}
	idx, err := readChoice(cmd, len(items))
	if err != nil {
		return err
	}
	ctx := items[idx].(contextItem).Context
	// drill compartments one level at a time
	parent := ctx.CompartmentOCID
	if parent == "" {
		parent = ctx.TenancyOCID
	}
	ociCfg := cfg.Options.OCIConfigPath
	for {
		fmt.Fprintf(cmd.OutOrStdout(), "Listing compartments under %s...\n", parent)
		citems, err := fetchPromptChildren(cmd, ctx, ociCfg, parent)
		if err != nil {
			return err
		}
		if len(citems) == 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "No child compartments; keeping current selection.")
			break
		}
		fmt.Fprintln(cmd.OutOrStdout(), "Select compartment (or 0 to keep current):")
		fmt.Fprintf(cmd.OutOrStdout(), "0) stay at %s\n", parent)
		for i, ci := range citems {
			marker := ""
			if ci.oc.Status != "ACTIVE" {
				marker = fmt.Sprintf(" [%s]", ci.oc.Status)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%d) %s%s\n", i+1, ci.oc.Name, marker)
		}
		cidx, err := readChoiceZero(cmd, len(citems))
		if err != nil {
			return err
		}
		if cidx == -1 {
			// 0 chosen
			break
		}
		parent = citems[cidx].oc.ID
	}
	ctx.CompartmentOCID = parent
	cfg.CurrentContext = ctx.Name
	if err := cfg.UpsertContext(ctx); err != nil {
		return err
	}
	if err := config.Save(path, cfg); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Selected context %s with compartment %s\n", ctx.Name, parent)
	return nil
}

// fetchPromptChildren mirrors the TUI lazy compartment fetch for the non-TTY prompt flow.
func fetchPromptChildren(cmd *cobra.Command, ctx config.Context, ociCfgPath string, parent string) ([]compItem, error) {
	c, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
	defer cancel()
	children, err := oci.FetchCompartments(c, ociCfgPath, ctx.Profile, ctx.Region, parent)
	if err != nil {
		return nil, err
	}
	items := make([]compItem, 0, len(children))
	for _, child := range children {
		items = append(items, compItem{oc: child})
	}
	return items, nil
}

func readChoice(cmd *cobra.Command, n int) (int, error) {
	var choice int
	if _, err := fmt.Fscan(cmd.InOrStdin(), &choice); err != nil {
		return 0, err
	}
	if choice < 1 || choice > n {
		return 0, fmt.Errorf("invalid choice")
	}
	return choice - 1, nil
}

func readChoiceZero(cmd *cobra.Command, n int) (int, error) {
	var choice int
	if _, err := fmt.Fscan(cmd.InOrStdin(), &choice); err != nil {
		return 0, err
	}
	if choice == 0 {
		return -1, nil
	}
	if choice < 1 || choice > n {
		return 0, fmt.Errorf("invalid choice")
	}
	return choice - 1, nil
}

type contextItem struct{ config.Context }

func (c contextItem) Title() string { return c.Name }
func (c contextItem) Description() string {
	return fmt.Sprintf("profile=%s region=%s", c.Profile, c.Region)
}
func (c contextItem) FilterValue() string { return c.Name }

// tenancyItem represents a tenancy grouped from profiles.
type tenancyItem struct {
	TenancyOCID string
	Profiles    []string // profile names belonging to this tenancy
	Name        string   // friendly tenancy name (if available)
}

func (t tenancyItem) Title() string {
	if t.Name != "" {
		return t.Name
	}
	return abbreviateOCID(t.TenancyOCID)
}

func (t tenancyItem) Description() string {
	return t.TenancyOCID
}

func (t tenancyItem) FilterValue() string {
	return t.TenancyOCID
}

type compItem struct {
	oc oci.Compartment
}

func (c compItem) Title() string {
	state := c.oc.Status
	marker := ""
	if state != "ACTIVE" {
		marker = fmt.Sprintf(" [%s]", state)
	}
	return fmt.Sprintf("%s%s", c.oc.Name, marker)
}
func (c compItem) Description() string { return c.oc.ID }
func (c compItem) FilterValue() string { return c.oc.Name }

type regionItem struct {
	name string
}

func (r regionItem) Title() string       { return r.name }
func (r regionItem) Description() string { return r.name }
func (r regionItem) FilterValue() string { return r.name }

type tuiModel struct {
	list               list.Model
	tenancies          list.Model
	cfg                config.Config
	cfgPath            string
	selected           string
	profiles           map[string]ocicfg.Profile
	err                error
	mode               string // "contexts", "compartments", "regions", or "tenancies"
	ctxItem            contextItem
	comps              list.Model
	regions            list.Model
	parentCrumb        string
	compCache          map[string][]compItem
	parentID           string
	parentMap          map[string]string // childID -> parentID
	nameMap            map[string]string // id -> display name
	status             string
	finalized          bool
	crumb              string
	regionSet          bool
	regionCache        map[string][]string // context name -> regions
	pendingSelectionID string              // compartment pending ID
	pendingSelectionNm string              // compartment pending name
	pendingRegion      string              // region pending name
	pendingContextName string              // context pending name
	pendingTenancyOCID string              // tenancy pending OCID
	ultraCompact       bool                // minimal chrome mode
	initCmd            tea.Cmd             // optional startup command for shortcut modes
}

func newTuiModel(cfg config.Config, cfgPath string, items []list.Item, profiles map[string]ocicfg.Profile, startMode string) tuiModel {
	// Set a reasonable default size to avoid zero-height rendering when no resize event arrives.
	defaultWidth, defaultHeight := 80, 20
	if w, h, err := term.GetSize(int(os.Stdout.Fd())); err == nil {
		if w > 0 {
			defaultWidth = w
		}
		if h > 0 {
			defaultHeight = h - 2 // leave a little padding
		}
	}
	if defaultWidth < 40 {
		defaultWidth = 40
	}
	if defaultHeight < 10 {
		defaultHeight = 10
	}
	l := list.New(items, list.NewDefaultDelegate(), defaultWidth, defaultHeight)
	l.Title = "Select OCI context"
	l.SetFilteringEnabled(true)
	l.SetShowHelp(false)
	l.SetShowStatusBar(false)
	tn := list.New(nil, list.NewDefaultDelegate(), defaultWidth, defaultHeight)
	tn.Title = "Select tenancy"
	tn.SetFilteringEnabled(true)
	tn.SetShowHelp(false)
	tn.SetShowStatusBar(false)
	if len(profiles) > 0 {
		// Try to pre-populate tenancy friendly names using identity calls so titles show names immediately.
		primeTenancyNames(context.Background(), profiles, cfg.Options.OCIConfigPath)
		tn.SetItems(tenanciesFromProfiles(profiles))
	}
	// Preselect current context if present
	if cfg.CurrentContext != "" {
		for i, it := range items {
			if ci, ok := it.(contextItem); ok && ci.Name == cfg.CurrentContext {
				l.Select(i)
				break
			}
		}
	}
	cl := list.New(nil, list.NewDefaultDelegate(), defaultWidth, defaultHeight)
	cl.Title = "Select compartment (lazy load)"
	cl.SetFilteringEnabled(true)
	cl.SetShowHelp(false)
	cl.SetShowStatusBar(false)
	// delegate with pending highlight is attached after model creation
	rl := list.New(nil, list.NewDefaultDelegate(), defaultWidth, defaultHeight)
	rl.Title = "Select region"
	rl.SetFilteringEnabled(true)
	rl.SetShowHelp(false)
	rl.SetShowStatusBar(false)
	m := tuiModel{list: l, tenancies: tn, cfg: cfg, cfgPath: cfgPath, mode: "contexts", profiles: profiles, comps: cl, regions: rl, compCache: make(map[string][]compItem), parentMap: make(map[string]string), nameMap: make(map[string]string), regionCache: make(map[string][]string)}
	m.refreshDelegates()
	m.applyStartMode(startMode)
	return m
}

func (m *tuiModel) refreshDelegates() {
	m.list.SetDelegate(newContextDelegate(&m.pendingContextName, m.ultraCompact))
	m.tenancies.SetDelegate(newTenancyDelegate(&m.pendingTenancyOCID, m.ultraCompact))
	m.comps.SetDelegate(newCompDelegate(&m.pendingSelectionID, m.ultraCompact))
	m.regions.SetDelegate(newRegionDelegate(&m.pendingRegion, m.ultraCompact))
	m.applyDensityMode()
}

func (m *tuiModel) applyDensityMode() {
	if m.ultraCompact {
		m.list.Title = ""
		m.tenancies.Title = ""
		m.comps.Title = ""
		m.regions.Title = ""
		return
	}
	m.list.Title = "Select OCI context"
	m.tenancies.Title = "Select tenancy"
	if m.parentCrumb != "" {
		m.comps.Title = fmt.Sprintf("Select compartment under %s", m.parentCrumb)
	} else {
		m.comps.Title = "Select compartment (lazy load)"
	}
	m.regions.Title = "Select region"
}

// selectInitialContext picks the current context if present, else the first context item.
func selectInitialContext(items []list.Item, current string) (contextItem, bool) {
	if current != "" {
		for _, it := range items {
			if ci, ok := it.(contextItem); ok && ci.Name == current {
				return ci, true
			}
		}
	}
	for _, it := range items {
		if ci, ok := it.(contextItem); ok {
			return ci, true
		}
	}
	return contextItem{}, false
}

// applyStartMode primes the model for the requested starting menu (contexts/compartments/regions/tenancies).
func (m *tuiModel) applyStartMode(startMode string) {
	mode := strings.ToLower(strings.TrimSpace(startMode))
	switch mode {
	case "", "context", "contexts":
		// default: nothing to do
		return
	case "region", "regions":
		if ctx, ok := selectInitialContext(m.list.Items(), m.cfg.CurrentContext); ok {
			m.ctxItem = ctx
			m.mode = "regions"
			m.status = "Loading regions..."
			m.initCmd = m.loadRegionsCmd(ctx)
			return
		}
	case "compartment", "compartments":
		if ctx, ok := selectInitialContext(m.list.Items(), m.cfg.CurrentContext); ok {
			m.ctxItem = ctx
			parent := ctx.CompartmentOCID
			if parent == "" {
				parent = ctx.TenancyOCID
			}
			m.parentID = parent
			m.parentCrumb = parentLabel(parent, ctx)
			m.parentMap[parent] = ctx.TenancyOCID
			m.nameMap[parent] = m.parentCrumb
			m.nameMap[ctx.TenancyOCID] = parentLabel(ctx.TenancyOCID, ctx)
			m.mode = "compartments"
			m.status = "Loading compartments..."
			m.crumb = fmt.Sprintf("Current: %s (%s)", m.parentCrumb, parent)
			m.initCmd = m.loadCompsCmd(parent)
			return
		}
	case "tenancy", "tenancies":
		if len(m.tenancies.Items()) > 0 {
			m.mode = "tenancies"
			m.status = "Select tenancy (Enter to use a profile and open root)"
			return
		}
	}
	// fallback if no contexts/tenancies available: stay in default mode
}

// goUpOne navigates to the known parent using recorded parent relationships.
func (m tuiModel) goUpOne() (tea.Model, tea.Cmd) {
	// If already at tenancy root, go back to contexts instead of reloading root.
	if m.parentID == m.ctxItem.TenancyOCID {
		m.mode = "contexts"
		m.status = ""
		m.crumb = ""
		return m, nil
	}
	parent := m.parentMap[m.parentID]
	if parent == "" {
		// If we don't know the parent, fall back to tenancy.
		parent = m.ctxItem.TenancyOCID
	}
	m.parentID = parent
	if name := m.nameMap[parent]; name != "" {
		m.parentCrumb = name
	} else {
		m.parentCrumb = parentLabel(parent, m.ctxItem)
	}
	m.status = "Loading compartments..."
	m.crumb = fmt.Sprintf("Current: %s (%s)", m.parentCrumb, m.parentID)
	return m, m.loadCompsCmd(m.parentID)
}

func (m tuiModel) Init() tea.Cmd {
	return m.initCmd
}

func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	m.refreshDelegates()
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		if m.mode == "contexts" {
			m.list.SetSize(msg.Width, msg.Height)
		} else if m.mode == "compartments" {
			m.comps.SetSize(msg.Width, msg.Height)
		} else if m.mode == "tenancies" {
			m.tenancies.SetSize(msg.Width, msg.Height)
		} else if m.mode == "regions" {
			m.regions.SetSize(msg.Width, msg.Height)
		}
	case tea.KeyMsg:
		// If currently filtering, route all keys except Enter through the active list to avoid triggering hotkeys.
		if m.mode == "contexts" && m.list.FilterState() == list.Filtering && msg.String() != "enter" {
			m.list, cmd = m.list.Update(msg)
			return m, cmd
		}
		if m.mode == "tenancies" && m.tenancies.FilterState() == list.Filtering && msg.String() != "enter" {
			m.tenancies, cmd = m.tenancies.Update(msg)
			return m, cmd
		}
		if m.mode == "compartments" && m.comps.FilterState() == list.Filtering && msg.String() != "enter" {
			m.comps, cmd = m.comps.Update(msg)
			return m, cmd
		}
		if m.mode == "regions" && m.regions.FilterState() == list.Filtering && msg.String() != "enter" {
			m.regions, cmd = m.regions.Update(msg)
			return m, cmd
		}

		switch msg.String() {
		case "enter", "right":
			// If currently filtering, apply the filtered subset and exit filter mode before acting.
			if m.mode == "contexts" && m.list.FilterState() == list.Filtering {
				vis := m.list.VisibleItems()
				m.list.SetItems(vis)
				m.list.SetFilteringEnabled(false)
				if len(vis) > 0 {
					m.list.Select(0)
				}
				return m, nil
			}
			if m.mode == "tenancies" && m.tenancies.FilterState() == list.Filtering {
				vis := m.tenancies.VisibleItems()
				m.tenancies.SetItems(vis)
				m.tenancies.SetFilteringEnabled(false)
				if len(vis) > 0 {
					m.tenancies.Select(0)
				}
				return m, nil
			}
			if m.mode == "compartments" && m.comps.FilterState() == list.Filtering {
				vis := m.comps.VisibleItems()
				m.comps.SetItems(vis)
				m.comps.SetFilteringEnabled(false)
				if len(vis) > 0 {
					m.comps.Select(0)
				}
				return m, nil
			}
			if m.mode == "regions" && m.regions.FilterState() == list.Filtering {
				vis := m.regions.VisibleItems()
				m.regions.SetItems(vis)
				m.regions.SetFilteringEnabled(false)
				if len(vis) > 0 {
					m.regions.Select(0)
				}
				return m, nil
			}
			if m.mode == "contexts" {
				if item, ok := m.list.SelectedItem().(contextItem); ok {
					m.ctxItem = item
					m.pendingSelectionID = ""
					m.pendingSelectionNm = ""
					m.pendingRegion = ""
					// start at current compartment if set, else tenancy
					parent := item.CompartmentOCID
					if parent == "" {
						parent = item.TenancyOCID
					}
					// reset maps for new session
					m.parentMap = make(map[string]string)
					m.nameMap = make(map[string]string)
					m.parentID = parent
					m.parentCrumb = parentLabel(parent, item)
					m.parentMap[parent] = item.TenancyOCID
					m.nameMap[parent] = m.parentCrumb
					m.nameMap[item.TenancyOCID] = parentLabel(item.TenancyOCID, item)
					m.mode = "compartments"
					m.status = "Loading compartments..."
					m.crumb = fmt.Sprintf("Current: %s (%s)", m.parentCrumb, parent)
					return m, m.loadCompsCmd(parent)
				}
			} else if m.mode == "tenancies" {
				if len(m.tenancies.Items()) == 0 {
					m.status = "No tenancies available"
					return m, nil
				}
				if item, ok := m.tenancies.SelectedItem().(tenancyItem); ok {
					profileName := selectProfileForTenancy(item, m.profiles, m.cfg.Options.DefaultProfile)
					p, ok := m.profiles[profileName]
					if !ok {
						m.status = fmt.Sprintf("profile %s not found", profileName)
						return m, nil
					}
					m.ctxItem = contextItemForProfile(profileName, p)
					m.pendingSelectionID = ""
					m.pendingSelectionNm = ""
					m.pendingRegion = ""
					// try to align selection in contexts list for visibility
					for i, it := range m.list.Items() {
						if ci, ok := it.(contextItem); ok && ci.Name == profileName {
							m.list.Select(i)
							break
						}
					}
					parent := item.TenancyOCID
					m.parentMap = make(map[string]string)
					m.nameMap = make(map[string]string)
					m.parentID = parent
					m.parentCrumb = parentLabel(parent, m.ctxItem)
					m.parentMap[parent] = item.TenancyOCID
					m.nameMap[parent] = m.parentCrumb
					m.nameMap[item.TenancyOCID] = parentLabel(item.TenancyOCID, m.ctxItem)
					m.mode = "compartments"
					m.status = "Loading compartments..."
					m.crumb = fmt.Sprintf("Current: %s (%s)", m.parentCrumb, parent)
					return m, m.loadCompsCmd(parent)
				}
				return m, nil
			} else if m.mode == "compartments" {
				// compartments mode
				if len(m.comps.Items()) == 0 {
					return m.finalizeSelection()
				}
				if item, ok := m.comps.SelectedItem().(compItem); ok {
					m.parentID = item.oc.ID
					m.parentCrumb = item.oc.Name
					m.nameMap[item.oc.ID] = item.oc.Name
					m.parentMap[item.oc.ID] = item.oc.Parent
					m.pendingSelectionID = ""
					m.pendingSelectionNm = ""
					m.status = "Loading compartments..."
					m.crumb = fmt.Sprintf("Current: %s (%s)", m.parentCrumb, m.parentID)
					return m, m.loadCompsCmd(item.oc.ID)
				}
			} else if m.mode == "regions" {
				if len(m.regions.Items()) == 0 {
					return m, nil
				}
				if item, ok := m.regions.SelectedItem().(regionItem); ok {
					m.ctxItem.Region = item.name
					m.regionSet = true
					m.mode = "contexts"
					m.status = fmt.Sprintf("Region set to %s (not saved until finalize)", item.name)
					return m, nil
				}
			}
		case " ":
			// Space acts per mode: mark pending selection with highlight and allow quick save.
			if m.mode == "contexts" {
				if item, ok := m.list.SelectedItem().(contextItem); ok {
					m.ctxItem = item
					m.pendingContextName = item.Name
					m.pendingSelectionID = ""
					m.pendingSelectionNm = ""
					m.pendingRegion = ""
					m.pendingTenancyOCID = ""
					parent := item.CompartmentOCID
					if parent == "" {
						parent = item.TenancyOCID
					}
					m.parentID = parent
					m.parentCrumb = parentLabel(parent, item)
					m.status = fmt.Sprintf("Context %s selected (pending save; Ctrl+S to save)", item.Name)
				}
				return m, nil
			}
			if m.mode == "tenancies" {
				if item, ok := m.tenancies.SelectedItem().(tenancyItem); ok {
					m.pendingTenancyOCID = item.TenancyOCID
					profileName := selectProfileForTenancy(item, m.profiles, m.cfg.Options.DefaultProfile)
					p, ok := m.profiles[profileName]
					if ok {
						m.ctxItem = contextItemForProfile(profileName, p)
						m.pendingContextName = profileName
						m.pendingSelectionID = ""
						m.pendingSelectionNm = ""
						m.pendingRegion = ""
						m.parentID = item.TenancyOCID
						m.parentCrumb = parentLabel(item.TenancyOCID, m.ctxItem)
						m.status = fmt.Sprintf("Tenancy %s selected (pending save; Ctrl+S to save)", abbreviateOCID(item.TenancyOCID))
					}
				}
				return m, nil
			}
			if m.mode == "compartments" {
				if item, ok := m.comps.SelectedItem().(compItem); ok {
					m.parentID = item.oc.ID
					m.parentCrumb = item.oc.Name
					m.nameMap[item.oc.ID] = item.oc.Name
					m.parentMap[item.oc.ID] = item.oc.Parent
					m.pendingSelectionID = item.oc.ID
					m.pendingSelectionNm = item.oc.Name
					m.status = fmt.Sprintf("Selected %s (pending save; Enter/right to drill, Ctrl+S/q to save)", item.oc.Name)
				}
				return m, nil
			}
			if m.mode == "regions" {
				if item, ok := m.regions.SelectedItem().(regionItem); ok {
					m.ctxItem.Region = item.name
					m.regionSet = true
					m.pendingRegion = item.name
					m.status = fmt.Sprintf("Region set to %s (pending save; Ctrl+S/q to save)", item.name)
				}
				return m, nil
			}
			return m, nil
		case "ctrl+s":
			return m.saveAndQuitCurrentMode()
		case "q":
			return m.saveAndQuitCurrentMode()
		case "esc", "ctrl+c":
			// Exit without saving on explicit quit keys.
			return m, tea.Quit
		case "b":
			// Lowercase hotkeys are only honored from the main menu.
			if m.mode == "contexts" {
				m.mode = "contexts"
				m.status = ""
			}
		case "delete", "backspace":
			// Allow editing filter; only treat as back-navigation when not filtering.
			if m.mode == "compartments" && m.comps.FilterState() != list.Filtering {
				m.status = "Loading parent..."
				return m.goUpOne()
			}
			if m.mode == "tenancies" && m.tenancies.FilterState() != list.Filtering {
				m.mode = "contexts"
				m.pendingSelectionID = ""
				m.pendingSelectionNm = ""
				m.status = ""
				return m, nil
			}
			if m.mode == "regions" && m.regions.FilterState() != list.Filtering {
				m.mode = "contexts"
				m.status = ""
				return m, nil
			}
		case "r":
			if m.mode == "contexts" {
				// open region picker for highlighted context
				if item, ok := m.list.SelectedItem().(contextItem); ok {
					m.ctxItem = item
					m.mode = "regions"
					m.status = "Loading regions..."
					if cached, ok := m.regionCache[item.Name]; ok {
						m.regions.SetItems(toRegionList(cached))
						m.regions.Select(0)
						m.status = "Select region (Space to stage, Ctrl+S to save)"
						return m, nil
					}
					return m, m.loadRegionsCmd(item)
				}
			}
		case "R":
			// Regions picker from any submenu using uppercase
			if m.mode != "contexts" {
				m.mode = "regions"
				m.status = "Loading regions..."
				if cached, ok := m.regionCache[m.ctxItem.Name]; ok {
					m.regions.SetItems(toRegionList(cached))
					m.regions.Select(0)
					m.status = "Select region (Space to stage, Ctrl+S to save)"
					return m, nil
				}
				return m, m.loadRegionsCmd(m.ctxItem)
			}
		case "c":
			// From contexts: open compartments
			if m.mode == "contexts" {
				if item, ok := m.list.SelectedItem().(contextItem); ok {
					m.ctxItem = item
					parent := item.CompartmentOCID
					if parent == "" {
						parent = item.TenancyOCID
					}
					m.parentMap = make(map[string]string)
					m.nameMap = make(map[string]string)
					m.parentID = parent
					m.parentCrumb = parentLabel(parent, item)
					m.parentMap[parent] = item.TenancyOCID
					m.nameMap[parent] = m.parentCrumb
					m.nameMap[item.TenancyOCID] = parentLabel(item.TenancyOCID, item)
					m.mode = "compartments"
					m.status = "Loading compartments..."
					m.crumb = fmt.Sprintf("Current: %s (%s)", m.parentCrumb, parent)
					return m, m.loadCompsCmd(parent)
				}
			}
		case "C":
			// From any submenu: go to compartments for the current context/tenancy
			if m.mode != "contexts" {
				// ensure ctxItem is set; pick initial if needed
				if (m.ctxItem == contextItem{}) {
					if ctx, ok := selectInitialContext(m.list.Items(), m.cfg.CurrentContext); ok {
						m.ctxItem = ctx
					}
				}
				parent := m.ctxItem.CompartmentOCID
				if parent == "" {
					parent = m.ctxItem.TenancyOCID
				}
				m.parentMap = make(map[string]string)
				m.nameMap = make(map[string]string)
				m.parentID = parent
				m.parentCrumb = parentLabel(parent, m.ctxItem)
				m.parentMap[parent] = m.ctxItem.TenancyOCID
				m.nameMap[parent] = m.parentCrumb
				m.nameMap[m.ctxItem.TenancyOCID] = parentLabel(m.ctxItem.TenancyOCID, m.ctxItem)
				m.mode = "compartments"
				m.status = "Loading compartments..."
				m.crumb = fmt.Sprintf("Current: %s (%s)", m.parentCrumb, parent)
				return m, m.loadCompsCmd(parent)
			}
		case "t":
			// Tenancies: only valid from main contexts menu
			if m.mode == "contexts" {
				if len(m.tenancies.Items()) == 0 {
					m.status = "No tenancies available"
					return m, nil
				}
				m.mode = "tenancies"
				m.status = "Select tenancy (Enter to use a profile and open root)"
				return m, nil
			}
		case "T":
			// Tenancies from submenus require uppercase
			if m.mode != "contexts" {
				if len(m.tenancies.Items()) == 0 {
					m.status = "No tenancies available"
					return m, nil
				}
				m.mode = "tenancies"
				m.status = "Select tenancy (Enter to use a profile and open root)"
				return m, nil
			}
		case "p":
			// Profiles/main list shortcut from contexts
			if m.mode == "contexts" {
				m.status = ""
				return m, nil
			}
		case "P":
			// Profiles/main list shortcut from any submenu
			if m.mode != "contexts" {
				m.mode = "contexts"
				m.status = ""
				m.crumb = ""
				return m, nil
			}
		case "/":
			// Enable filtering explicitly via '/'; do not auto-start on arbitrary keys.
			// Clear the filter text and put the list into filtering state so typing works.
			if m.mode == "contexts" {
				m.list.SetFilteringEnabled(true)
				m.list.SetFilterText("")
				m.list.SetFilterState(list.Filtering)
			}
			if m.mode == "compartments" {
				m.comps.SetFilteringEnabled(true)
				m.comps.SetFilterText("")
				m.comps.SetFilterState(list.Filtering)
			}
			if m.mode == "tenancies" {
				m.tenancies.SetFilteringEnabled(true)
				m.tenancies.SetFilterText("")
				m.tenancies.SetFilterState(list.Filtering)
			}
			if m.mode == "regions" {
				m.regions.SetFilteringEnabled(true)
				m.regions.SetFilterText("")
				m.regions.SetFilterState(list.Filtering)
			}
			return m, nil
		case "u":
			m.ultraCompact = !m.ultraCompact
			m.applyDensityMode()
			if m.ultraCompact {
				m.status = "ULTRA mode: ON"
			} else {
				m.status = "ULTRA mode: OFF"
			}
			return m, nil
		}
	}
	// handle async comp results
	if res, ok := msg.(compResultMsg); ok {
		if res.err != nil {
			m.err = res.err
			return m, tea.Quit
		}
		m.compCache[res.parent] = res.items
		for _, it := range res.items {
			m.parentMap[it.oc.ID] = it.oc.Parent
			m.nameMap[it.oc.ID] = it.oc.Name
		}
		m.comps.SetItems(toList(res.items))
		m.comps.Title = fmt.Sprintf("Select compartment under %s", res.parent)
		if len(res.items) == 0 {
			m.status = "Leaf compartment: press backspace/delete to go up, or Enter/Space/Ctrl+S to keep current."
		} else {
			m.status = ""
		}
	}
	if res, ok := msg.(regionResultMsg); ok {
		if res.err != nil {
			// fallback to static regions but keep the error in status for visibility
			m.status = fmt.Sprintf("Region fetch failed: %v (showing defaults)", res.err)
			m.regionCache[res.ctxName] = fallbackRegions
			m.regions.SetItems(toRegionList(fallbackRegions))
			m.regions.Select(0)
			return m, nil
		}
		m.regionCache[res.ctxName] = res.items
		items := res.items
		if len(items) == 0 {
			items = fallbackRegions
		}
		m.regions.SetItems(toRegionList(items))
		m.regions.Select(0)
		m.status = "Select region (Space to stage, Ctrl+S to save)"
		return m, nil
	}
	if m.mode == "contexts" {
		m.list, cmd = m.list.Update(msg)
		return m, cmd
	}
	if m.mode == "tenancies" {
		m.tenancies, cmd = m.tenancies.Update(msg)
		return m, cmd
	}
	if m.mode == "compartments" {
		m.comps, cmd = m.comps.Update(msg)
		return m, cmd
	}
	m.regions, cmd = m.regions.Update(msg)
	return m, cmd
}

func (m tuiModel) View() string {
	m.refreshDelegates()
	if m.err != nil {
		return fmt.Sprintf("error: %v", m.err)
	}
	if m.finalized {
		return fmt.Sprintf("Selected context %s with compartment %s\n", m.ctxItem.Name, m.parentID)
	}
	if m.mode == "contexts" {
		meta := compactMeta(m)
		if m.ultraCompact {
			return fmt.Sprintf("%s\n%s", lipgloss.NewStyle().Foreground(infoColor).Render("[ULTRA] "+meta), m.list.View())
		}
		instructions := "ctx | enter drill • space stage • r regions • t tenancies • / filter • u ultra • q save • esc quit"
		return fmt.Sprintf("%s\n%s\n%s", lipgloss.NewStyle().Foreground(infoColor).Render(instructions), lipgloss.NewStyle().Foreground(infoColor).Render(meta), m.list.View())
	}
	if m.mode == "tenancies" {
		meta := compactMeta(m)
		view := m.tenancies.View()
		if m.status != "" {
			view = fmt.Sprintf("%s\n%s", m.status, view)
		}
		if m.ultraCompact {
			return fmt.Sprintf("%s\n%s", lipgloss.NewStyle().Foreground(infoColor).Render("[ULTRA] "+meta), view)
		}
		instructions := "tenancy | enter use • space stage • backspace/P back • / filter • u ultra • q save • esc quit"
		return fmt.Sprintf("%s\n%s\n%s", lipgloss.NewStyle().Foreground(infoColor).Render(instructions), lipgloss.NewStyle().Foreground(infoColor).Render(meta), view)
	}
	if m.mode == "regions" {
		meta := compactMeta(m)
		view := m.regions.View()
		if m.status != "" {
			view = fmt.Sprintf("%s\n%s", m.status, view)
		}
		if m.ultraCompact {
			return fmt.Sprintf("%s\n%s", lipgloss.NewStyle().Foreground(infoColor).Render("[ULTRA] "+meta), view)
		}
		instructions := "region | space stage • enter apply+back • backspace/P back • / filter • u ultra • q save • esc quit"
		return fmt.Sprintf("%s\n%s\n%s", lipgloss.NewStyle().Foreground(infoColor).Render(instructions), lipgloss.NewStyle().Foreground(infoColor).Render(meta), view)
	}
	meta := compactMeta(m)
	view := m.comps.View()
	if m.crumb != "" {
		view = fmt.Sprintf("%s\n%s", m.crumb, view)
	}
	if m.status != "" {
		view = fmt.Sprintf("%s\n%s", m.status, view)
	}
	if m.ultraCompact {
		return fmt.Sprintf("%s\n%s", lipgloss.NewStyle().Foreground(infoColor).Render("[ULTRA] "+meta), view)
	}
	instructions := "comp | enter drill • space stage • backspace up • / filter • u ultra • q save • esc quit"
	return fmt.Sprintf("%s\n%s\n%s", lipgloss.NewStyle().Foreground(infoColor).Render(instructions), lipgloss.NewStyle().Foreground(infoColor).Render(meta), view)
}

func compactMeta(m tuiModel) string {
	staged := "-"
	if m.pendingContextName != "" {
		staged = "ctx:" + m.pendingContextName
	}
	if m.pendingTenancyOCID != "" {
		staged = "tenancy:" + abbreviateOCID(m.pendingTenancyOCID)
	}
	if m.pendingSelectionID != "" {
		staged = "comp:" + abbreviateOCID(m.pendingSelectionID)
	}
	if m.pendingRegion != "" {
		staged = "region:" + m.pendingRegion
	}
	filter := "off"
	switch m.mode {
	case "contexts":
		if m.list.FilterState() == list.Filtering {
			filter = "on"
		}
	case "tenancies":
		if m.tenancies.FilterState() == list.Filtering {
			filter = "on"
		}
	case "compartments":
		if m.comps.FilterState() == list.Filtering {
			filter = "on"
		}
	case "regions":
		if m.regions.FilterState() == list.Filtering {
			filter = "on"
		}
	}
	current := m.ctxItem.Name
	if current == "" {
		current = m.cfg.CurrentContext
	}
	if current == "" {
		current = "-"
	}
	return fmt.Sprintf("mode:%s | current:%s | staged:%s | filter:%s", m.mode, current, staged, filter)
}

type compResultMsg struct {
	parent string
	items  []compItem
	err    error
}

type regionResultMsg struct {
	ctxName string
	items   []string
	err     error
}

func (m tuiModel) loadRegionsCmd(ctxItem contextItem) tea.Cmd {
	return func() tea.Msg {
		c, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		regions, err := oci.ListRegionSubscriptions(c, m.cfg.Options.OCIConfigPath, ctxItem.Profile)
		return regionResultMsg{ctxName: ctxItem.Name, items: regions, err: err}
	}
}

func (m tuiModel) loadCompsCmd(parent string) tea.Cmd {
	return func() tea.Msg {
		// if cached, return cached without call
		if items, ok := m.compCache[parent]; ok {
			return compResultMsg{parent: parent, items: items}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		citems, err := m.fetchChildren(ctx, parent)
		return compResultMsg{parent: parent, items: citems, err: err}
	}
}

func (m tuiModel) fetchChildren(ctx context.Context, parent string) ([]compItem, error) {
	// use selected context's profile/region/tenancy
	selected := m.ctxItem.Context
	ociCfg := m.cfg.Options.OCIConfigPath
	children, err := oci.FetchCompartments(ctx, ociCfg, selected.Profile, selected.Region, parent)
	if err != nil {
		return nil, err
	}
	items := make([]compItem, 0, len(children))
	for _, c := range children {
		items = append(items, compItem{oc: c})
	}
	// if no children, return empty; caller will allow finalization
	return items, nil
}

// saveAndQuitCurrentMode consolidates save+exit behavior used by q and Ctrl+S.
func (m tuiModel) saveAndQuitCurrentMode() (tea.Model, tea.Cmd) {
	if m.mode == "contexts" {
		if item, ok := m.list.SelectedItem().(contextItem); ok {
			prevCtxItem := m.ctxItem
			m.ctxItem = item
			parent := ""
			// If a compartment is staged for the same context that was selected before
			// this save operation, preserve that staged compartment selection.
			if m.pendingSelectionID != "" && m.parentID != "" && prevCtxItem.Name == item.Name {
				parent = m.parentID
			}
			if parent == "" {
				parent = item.CompartmentOCID
				if parent == "" {
					parent = item.TenancyOCID
				}
			}
			m.parentID = parent
			m.parentCrumb = parentLabel(parent, item)
			return m.finalizeSelection()
		}
		return m, nil
	}
	if m.mode == "tenancies" {
		if item, ok := m.tenancies.SelectedItem().(tenancyItem); ok {
			profileName := selectProfileForTenancy(item, m.profiles, m.cfg.Options.DefaultProfile)
			p, ok := m.profiles[profileName]
			if ok {
				m.ctxItem = contextItemForProfile(profileName, p)
				m.parentID = item.TenancyOCID
				m.parentCrumb = parentLabel(item.TenancyOCID, m.ctxItem)
				return m.finalizeSelection()
			}
		}
		return m, nil
	}
	if m.mode == "compartments" {
		return m.finalizeSelection()
	}
	if m.mode == "regions" {
		if item, ok := m.regions.SelectedItem().(regionItem); ok {
			m.ctxItem.Region = item.name
			m.regionSet = true
			if m.parentID == "" {
				parent := m.ctxItem.CompartmentOCID
				if parent == "" {
					parent = m.ctxItem.TenancyOCID
				}
				m.parentID = parent
				m.parentCrumb = parentLabel(parent, m.ctxItem)
			}
			return m.finalizeSelection()
		}
	}
	return m, nil
}

// parentLabel returns a friendly label for the current parent (root/tenancy fallback).
func parentLabel(parent string, item contextItem) string {
	if parent == item.TenancyOCID {
		return "root"
	}
	return parent
}

// finalizeSelection sets the chosen compartment, saves config, and quits.
func (m tuiModel) finalizeSelection() (tea.Model, tea.Cmd) {
	m.finalized = true
	m.selected = m.ctxItem.Name
	// persist selection (compartment + region if set)
	m.ctxItem.CompartmentOCID = m.parentID
	// Region persisted by UpsertContext from ctxItem; regionSet already applied
	m.cfg.CurrentContext = m.ctxItem.Name
	if err := m.cfg.UpsertContext(m.ctxItem.Context); err != nil {
		m.err = err
		return m, tea.Quit
	}
	if err := config.Save(m.cfgPath, m.cfg); err != nil {
		m.err = err
		return m, tea.Quit
	}
	return m, tea.Quit
}

func toList(items []compItem) []list.Item {
	out := make([]list.Item, len(items))
	for i, it := range items {
		out[i] = it
	}
	return out
}
