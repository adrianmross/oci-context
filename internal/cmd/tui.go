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
	stagedColor      = lipgloss.Color("205")
	currentColor     = lipgloss.Color("39")
	infoColor        = lipgloss.Color("252")
	accentColor      = lipgloss.Color("45")
	activeTabColor   = lipgloss.Color("33")
	inactiveTabColor = lipgloss.Color("245")
	panelColor       = lipgloss.Color("240")
	mutedTextColor   = lipgloss.Color("242")
	statusOkColor    = lipgloss.Color("42")
	statusInfoColor  = lipgloss.Color("45")
	statusWarnColor  = lipgloss.Color("214")
	statusErrColor   = lipgloss.Color("196")
)

type tuiTheme struct {
	headerTitle  lipgloss.Style
	headerSubtle lipgloss.Style
	tabActive    lipgloss.Style
	tabInactive  lipgloss.Style
	panel        lipgloss.Style
	instructions lipgloss.Style
	metaLabel    lipgloss.Style
	metaValue    lipgloss.Style
	status       lipgloss.Style
	statusInfo   lipgloss.Style
	statusWarn   lipgloss.Style
	statusErr    lipgloss.Style
	statusMuted  lipgloss.Style
	ultraBadge   lipgloss.Style
	metaBar      lipgloss.Style
	gridCell     lipgloss.Style
	gridSelected lipgloss.Style
	gridStaged   lipgloss.Style
}

func newTUITheme() tuiTheme {
	return tuiTheme{
		headerTitle:  lipgloss.NewStyle().Foreground(accentColor).Bold(true),
		headerSubtle: lipgloss.NewStyle().Foreground(mutedTextColor),
		tabActive: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("230")).
			Background(activeTabColor).
			Padding(0, 1).
			MarginRight(1),
		tabInactive: lipgloss.NewStyle().
			Foreground(inactiveTabColor).
			Padding(0, 1).
			MarginRight(1),
		panel: lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(panelColor).
			Padding(0, 1),
		instructions: lipgloss.NewStyle().Foreground(mutedTextColor),
		metaLabel:    lipgloss.NewStyle().Foreground(mutedTextColor),
		metaValue:    lipgloss.NewStyle().Foreground(infoColor),
		status: lipgloss.NewStyle().
			Foreground(statusOkColor).
			Bold(true),
		statusInfo: lipgloss.NewStyle().
			Foreground(statusInfoColor).
			Bold(true),
		statusWarn: lipgloss.NewStyle().
			Foreground(statusWarnColor).
			Bold(true),
		statusErr: lipgloss.NewStyle().
			Foreground(statusErrColor).
			Bold(true),
		statusMuted: lipgloss.NewStyle().Foreground(mutedTextColor),
		ultraBadge: lipgloss.NewStyle().
			Foreground(infoColor).
			Bold(true),
		metaBar: lipgloss.NewStyle(),
		gridCell: lipgloss.NewStyle().
			Padding(0, 1),
		gridSelected: lipgloss.NewStyle().
			Foreground(lipgloss.Color("230")).
			Background(activeTabColor).
			Bold(true).
			Padding(0, 1),
		gridStaged: lipgloss.NewStyle().
			Foreground(stagedColor).
			Bold(true),
	}
}

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

const filterPlaceholderHint = "press esc to escape"

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
			items := profileMenuItems(cfg, profiles, perr)
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

type separatorItem struct{}

func (s separatorItem) Title() string       { return "" }
func (s separatorItem) Description() string { return "" }
func (s separatorItem) FilterValue() string { return "" }

type sectionItem struct {
	title string
}

func (s sectionItem) Title() string       { return s.title }
func (s sectionItem) Description() string { return "" }
func (s sectionItem) FilterValue() string { return "" }

// compDelegate wraps the default delegate to color the pending selection when present.
type compDelegate struct {
	list.DefaultDelegate
	pendingID    *string
	currentID    *string
	ultraCompact bool
}

type markedItem struct {
	base        list.Item
	title       string
	description string
}

func (m markedItem) Title() string       { return m.title }
func (m markedItem) Description() string { return m.description }
func (m markedItem) FilterValue() string { return m.base.FilterValue() }

func withStageMarker(item list.Item, ultraCompact bool) list.Item {
	title := itemTitle(item)
	description := itemDescription(item)
	if ultraCompact {
		return markedItem{base: item, title: "[*] " + title, description: description}
	}
	badge := lipgloss.NewStyle().
		Foreground(lipgloss.Color("230")).
		Background(stagedColor).
		Bold(true).
		Padding(0, 1).
		Render("STAGED")
	return markedItem{base: item, title: fmt.Sprintf("%s  %s", title, badge), description: description}
}

func withCurrentMarker(item list.Item, ultraCompact bool) list.Item {
	title := itemTitle(item)
	description := itemDescription(item)
	if ultraCompact {
		return markedItem{base: item, title: "[=] " + title, description: description}
	}
	badge := lipgloss.NewStyle().
		Foreground(lipgloss.Color("230")).
		Background(currentColor).
		Bold(true).
		Padding(0, 1).
		Render("CURRENT")
	return markedItem{base: item, title: fmt.Sprintf("%s  %s", title, badge), description: description}
}

func itemTitle(item list.Item) string {
	switch it := item.(type) {
	case markedItem:
		return it.Title()
	case contextItem:
		return it.Title()
	case sectionItem:
		return it.Title()
	case separatorItem:
		return ""
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

func itemDescription(item list.Item) string {
	switch it := item.(type) {
	case markedItem:
		return it.Description()
	case contextItem:
		return it.Description()
	case sectionItem:
		return it.Description()
	case separatorItem:
		return ""
	case tenancyItem:
		return it.Description()
	case compItem:
		return it.Description()
	case regionItem:
		return it.Description()
	default:
		return ""
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

func applyDelegateTheme(d *list.DefaultDelegate) {
	normalTitle := lipgloss.NewStyle().Foreground(infoColor)
	normalDesc := lipgloss.NewStyle().Foreground(mutedTextColor)
	selectedTitle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("230")).
		Background(activeTabColor).
		Bold(true)
	selectedDesc := lipgloss.NewStyle().Foreground(accentColor)

	d.Styles.NormalTitle = normalTitle
	d.Styles.NormalDesc = normalDesc
	d.Styles.SelectedTitle = selectedTitle
	d.Styles.SelectedDesc = selectedDesc
	d.Styles.DimmedTitle = normalTitle.Foreground(mutedTextColor)
	d.Styles.DimmedDesc = normalDesc
	d.Styles.FilterMatch = lipgloss.NewStyle().Foreground(accentColor).Bold(true)
}

func newCompDelegate(pendingID *string, currentID *string, ultraCompact bool) *compDelegate {
	d := list.NewDefaultDelegate()
	configureDefaultDelegateDensity(&d, ultraCompact)
	applyDelegateTheme(&d)
	return &compDelegate{
		DefaultDelegate: d,
		pendingID:       pendingID,
		currentID:       currentID,
		ultraCompact:    ultraCompact,
	}
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
		d.DefaultDelegate.Render(w, m, index, withStageMarker(listItem, d.ultraCompact))
		d.Styles.NormalTitle = origNormalTitle
		d.Styles.NormalDesc = origNormalDesc
		d.Styles.SelectedTitle = origTitle
		d.Styles.SelectedDesc = origDesc
		return
	}
	if ci, ok := listItem.(compItem); ok && d.currentID != nil && *d.currentID != "" && ci.oc.ID == *d.currentID {
		d.DefaultDelegate.Render(w, m, index, withCurrentMarker(listItem, d.ultraCompact))
		return
	}
	d.DefaultDelegate.Render(w, m, index, listItem)
}

// regionDelegate highlights pending region selection when present.
type regionDelegate struct {
	list.DefaultDelegate
	pendingName  *string
	currentName  *string
	ultraCompact bool
}

// contextDelegate highlights pending context selection when present.
type contextDelegate struct {
	list.DefaultDelegate
	pendingName  *string
	currentName  *string
	ultraCompact bool
}

func newContextDelegate(pendingName *string, currentName *string, ultraCompact bool) *contextDelegate {
	d := list.NewDefaultDelegate()
	configureDefaultDelegateDensity(&d, ultraCompact)
	applyDelegateTheme(&d)
	return &contextDelegate{
		DefaultDelegate: d,
		pendingName:     pendingName,
		currentName:     currentName,
		ultraCompact:    ultraCompact,
	}
}

func (d *contextDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	if _, ok := listItem.(separatorItem); ok {
		if d.ultraCompact {
			// In compact mode, draw only one divider at section boundaries.
			return
		}
		line := strings.Repeat("─", 36)
		fmt.Fprint(w, lipgloss.NewStyle().Foreground(panelColor).Render(line))
		return
	}
	if si, ok := listItem.(sectionItem); ok {
		if d.ultraCompact {
			// In compact mode, keep exactly one divider between CONTEXTS and PROFILES.
			if strings.EqualFold(si.title, "PROFILES") && index > 0 {
				line := strings.Repeat("─", 36)
				fmt.Fprint(w, lipgloss.NewStyle().Foreground(panelColor).Render(line))
			}
			return
		}
		fmt.Fprint(w, lipgloss.NewStyle().Foreground(mutedTextColor).Bold(true).Render(si.Title()))
		return
	}
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
		d.DefaultDelegate.Render(w, m, index, withStageMarker(listItem, d.ultraCompact))
		d.Styles.NormalTitle = origTitle
		d.Styles.NormalDesc = origDesc
		d.Styles.SelectedTitle = origSelectedTitle
		d.Styles.SelectedDesc = origSelectedDesc
		return
	}
	if ci, ok := listItem.(contextItem); ok && d.currentName != nil && *d.currentName != "" && ci.Name == *d.currentName {
		d.DefaultDelegate.Render(w, m, index, withCurrentMarker(listItem, d.ultraCompact))
		return
	}
	d.DefaultDelegate.Render(w, m, index, listItem)
}

// tenancyDelegate highlights pending tenancy selection when present.
type tenancyDelegate struct {
	list.DefaultDelegate
	pendingOCID  *string
	currentOCID  *string
	ultraCompact bool
}

func newTenancyDelegate(pendingOCID *string, currentOCID *string, ultraCompact bool) *tenancyDelegate {
	d := list.NewDefaultDelegate()
	configureDefaultDelegateDensity(&d, ultraCompact)
	applyDelegateTheme(&d)
	return &tenancyDelegate{
		DefaultDelegate: d,
		pendingOCID:     pendingOCID,
		currentOCID:     currentOCID,
		ultraCompact:    ultraCompact,
	}
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
		d.DefaultDelegate.Render(w, m, index, withStageMarker(listItem, d.ultraCompact))
		d.Styles.NormalTitle = origTitle
		d.Styles.NormalDesc = origDesc
		d.Styles.SelectedTitle = origSelectedTitle
		d.Styles.SelectedDesc = origSelectedDesc
		return
	}
	if ti, ok := listItem.(tenancyItem); ok && d.currentOCID != nil && *d.currentOCID != "" && ti.TenancyOCID == *d.currentOCID {
		d.DefaultDelegate.Render(w, m, index, withCurrentMarker(listItem, d.ultraCompact))
		return
	}
	d.DefaultDelegate.Render(w, m, index, listItem)
}

func newRegionDelegate(pendingName *string, currentName *string, ultraCompact bool) *regionDelegate {
	d := list.NewDefaultDelegate()
	configureDefaultDelegateDensity(&d, ultraCompact)
	applyDelegateTheme(&d)
	return &regionDelegate{
		DefaultDelegate: d,
		pendingName:     pendingName,
		currentName:     currentName,
		ultraCompact:    ultraCompact,
	}
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
		d.DefaultDelegate.Render(w, m, index, withStageMarker(listItem, d.ultraCompact))
		d.Styles.NormalTitle = origNormalTitle
		d.Styles.NormalDesc = origNormalDesc
		d.Styles.SelectedTitle = origTitle
		d.Styles.SelectedDesc = origDesc
		return
	}
	if ri, ok := listItem.(regionItem); ok && d.currentName != nil && *d.currentName != "" && ri.name == *d.currentName {
		d.DefaultDelegate.Render(w, m, index, withCurrentMarker(listItem, d.ultraCompact))
		return
	}
	d.DefaultDelegate.Render(w, m, index, listItem)
}

// contextsFromProfiles builds context items from OCI CLI profiles.
func contextsFromProfiles(profiles map[string]ocicfg.Profile, current config.Context, hasCurrent bool) []list.Item {
	names := make([]string, 0, len(profiles))
	for name := range profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	items := make([]list.Item, 0, len(names))
	for _, name := range names {
		p := profiles[name]
		ci := contextItem{Context: config.Context{
			Name:            name,
			Profile:         name,
			TenancyOCID:     p.Tenancy,
			CompartmentOCID: p.Tenancy,
			Region:          p.Region,
			User:            p.User,
		}}
		if hasCurrent && isContextEquivalentToNamedProfile(current, name, p) {
			ci.isCurrent = true
		}
		items = append(items, ci)
	}
	return items
}

func contextsFromConfig(cfg config.Config, profiles map[string]ocicfg.Profile) []list.Item {
	names := make([]string, 0, len(cfg.Contexts))
	byName := make(map[string]config.Context, len(cfg.Contexts))
	seenBySignature := make(map[string]string) // signature -> kept context name
	for _, c := range cfg.Contexts {
		if isContextEquivalentToProfile(c, profiles) {
			// Hide contexts that are effectively identical to an OCI profile baseline.
			continue
		}
		sig := contextSignature(c)
		if keptName, exists := seenBySignature[sig]; exists {
			// Prefer current context when two saved contexts are equivalent by effective values.
			if c.Name == cfg.CurrentContext {
				delete(byName, keptName)
				for i, n := range names {
					if n == keptName {
						names = append(names[:i], names[i+1:]...)
						break
					}
				}
				seenBySignature[sig] = c.Name
			} else {
				continue
			}
		} else {
			seenBySignature[sig] = c.Name
		}
		names = append(names, c.Name)
		byName[c.Name] = c
	}
	sort.Strings(names)
	if cfg.CurrentContext != "" {
		for i, n := range names {
			if n == cfg.CurrentContext {
				names = append([]string{n}, append(names[:i], names[i+1:]...)...)
				break
			}
		}
	}
	items := make([]list.Item, 0, len(names))
	for _, name := range names {
		items = append(items, contextItem{
			Context:   byName[name],
			fromSaved: true,
			isCurrent: name == cfg.CurrentContext,
		})
	}
	return items
}

func contextSignature(c config.Context) string {
	return strings.Join([]string{
		c.Profile,
		c.TenancyOCID,
		c.CompartmentOCID,
		c.Region,
	}, "|")
}

func profileMenuItems(cfg config.Config, profiles map[string]ocicfg.Profile, profilesErr error) []list.Item {
	return profileMenuItemsForDensity(cfg, profiles, profilesErr, true)
}

func profileMenuItemsForDensity(cfg config.Config, profiles map[string]ocicfg.Profile, profilesErr error, showSections bool) []list.Item {
	current, hasCurrent := cfg.GetContext(cfg.CurrentContext)
	profileItems := contextsFromProfiles(profiles, current, hasCurrent == nil)
	contextItems := contextsFromConfig(cfg, profiles)
	items := make([]list.Item, 0, len(profileItems)+len(contextItems)+4)

	if showSections {
		if len(contextItems) > 0 {
			items = append(items, sectionItem{title: "CONTEXTS"})
			items = append(items, contextItems...)
		}
		if len(profileItems) > 0 {
			if len(contextItems) > 0 {
				items = append(items, separatorItem{})
			}
			items = append(items, sectionItem{title: "PROFILES"})
			items = append(items, profileItems...)
		}
	} else {
		items = append(items, contextItems...)
		if len(contextItems) > 0 && len(profileItems) > 0 {
			items = append(items, separatorItem{})
		}
		items = append(items, profileItems...)
	}
	if len(items) > 0 {
		return items
	}
	// Ultimate fallback if both sources are empty/unavailable.
	if profilesErr != nil {
		return []list.Item{}
	}
	return profileItems
}

func (m *tuiModel) refreshContextMenuItems() {
	if !m.managedContextMenu {
		return
	}
	showSections := m.isModeVerbose("contexts")
	items := profileMenuItemsForDensity(m.cfg, m.profiles, nil, showSections)
	if len(items) == 0 {
		m.list.SetItems(items)
		return
	}
	selectedName := ""
	if selected, ok := m.list.SelectedItem().(contextItem); ok {
		selectedName = selected.Name
	}
	m.list.SetItems(items)
	if selectedName == "" {
		selectedName = m.cfg.CurrentContext
	}
	if selectedName != "" {
		for i, it := range items {
			if ci, ok := it.(contextItem); ok && ci.Name == selectedName {
				m.list.Select(i)
				return
			}
		}
	}
	for i, it := range items {
		if _, ok := it.(contextItem); ok {
			m.list.Select(i)
			return
		}
	}
}

func isContextEquivalentToProfile(c config.Context, profiles map[string]ocicfg.Profile) bool {
	profileName := c.Profile
	if profileName == "" {
		// Legacy contexts may omit profile but keep the context name identical to profile.
		profileName = c.Name
	}
	p, ok := profiles[profileName]
	if !ok {
		return false
	}
	tenancy := c.TenancyOCID
	if tenancy == "" {
		tenancy = p.Tenancy
	}
	region := c.Region
	if region == "" {
		region = p.Region
	}
	compartment := c.CompartmentOCID
	if compartment == "" {
		// Empty compartment in context means root/tenancy in this app.
		compartment = tenancy
	}
	return tenancy == p.Tenancy &&
		region == p.Region &&
		compartment == p.Tenancy
}

func isContextEquivalentToNamedProfile(c config.Context, profileName string, p ocicfg.Profile) bool {
	ctxProfile := c.Profile
	if ctxProfile == "" {
		ctxProfile = c.Name
	}
	if ctxProfile != profileName {
		return false
	}
	tenancy := c.TenancyOCID
	if tenancy == "" {
		tenancy = p.Tenancy
	}
	region := c.Region
	if region == "" {
		region = p.Region
	}
	compartment := c.CompartmentOCID
	if compartment == "" {
		compartment = tenancy
	}
	return tenancy == p.Tenancy && region == p.Region && compartment == p.Tenancy
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
	return contextItem{Context: config.Context{
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
	items := contextsFromProfiles(profiles, config.Context{}, false)
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

type contextItem struct {
	config.Context
	fromSaved bool
	isCurrent bool
}

func (c contextItem) Title() string {
	if c.isCurrent {
		if !c.fromSaved {
			return c.Name + " @CURRENT"
		}
		return "@CURRENT"
	}
	return c.Name
}
func (c contextItem) Description() string {
	if c.fromSaved {
		return fmt.Sprintf("context profile=%s region=%s", c.Profile, c.Region)
	}
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
	savedContextName   string              // context currently persisted on disk
	savedTenancyOCID   string              // tenancy currently persisted on disk
	savedCompartmentID string              // compartment currently persisted on disk
	savedRegion        string              // region currently persisted on disk
	ultraCompact       bool                // minimal chrome mode
	helpVisible        bool                // keybindings panel toggle
	initCmd            tea.Cmd             // optional startup command for shortcut modes
	theme              tuiTheme
	prefs              tuiPrefs
	prefsPath          string
	layoutOverride     string // "", "list", or "matrix"
	width              int
	height             int
	panelInnerHeight   int
	managedContextMenu bool
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
	prefs, prefsPath, prefsErr := loadTUIPrefs()
	if prefsErr != nil {
		prefs = defaultTUIPrefs()
		prefsPath = ""
	}

	l := list.New(items, list.NewDefaultDelegate(), defaultWidth, defaultHeight)
	l.Title = "Select OCI context"
	l.SetFilteringEnabled(true)
	l.FilterInput.Placeholder = filterPlaceholderHint
	l.SetShowHelp(false)
	l.SetShowStatusBar(false)
	tn := list.New(nil, list.NewDefaultDelegate(), defaultWidth, defaultHeight)
	tn.Title = "Select tenancy"
	tn.SetFilteringEnabled(true)
	tn.FilterInput.Placeholder = filterPlaceholderHint
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
	// If the selected row is a section header, move to the first actual context row.
	if _, ok := l.SelectedItem().(contextItem); !ok {
		for i, it := range items {
			if _, ok := it.(contextItem); ok {
				l.Select(i)
				break
			}
		}
	}
	cl := list.New(nil, list.NewDefaultDelegate(), defaultWidth, defaultHeight)
	cl.Title = "Select compartment (lazy load)"
	cl.SetFilteringEnabled(true)
	cl.FilterInput.Placeholder = filterPlaceholderHint
	cl.SetShowHelp(false)
	cl.SetShowStatusBar(false)
	// delegate with pending highlight is attached after model creation
	rl := list.New(nil, list.NewDefaultDelegate(), defaultWidth, defaultHeight)
	rl.Title = "Select region"
	rl.SetFilteringEnabled(true)
	rl.FilterInput.Placeholder = filterPlaceholderHint
	rl.SetShowHelp(false)
	rl.SetShowStatusBar(false)
	m := tuiModel{
		list:        l,
		tenancies:   tn,
		cfg:         cfg,
		cfgPath:     cfgPath,
		mode:        "contexts",
		profiles:    profiles,
		comps:       cl,
		regions:     rl,
		compCache:   make(map[string][]compItem),
		parentMap:   make(map[string]string),
		nameMap:     make(map[string]string),
		regionCache: make(map[string][]string),
		theme:       newTUITheme(),
		prefs:       prefs,
		prefsPath:   prefsPath,
		width:       defaultWidth,
		height:      defaultHeight,
	}
	if current, err := cfg.GetContext(cfg.CurrentContext); err == nil {
		m.savedContextName = cfg.CurrentContext
		m.savedTenancyOCID = current.TenancyOCID
		m.savedCompartmentID = current.CompartmentOCID
		if m.savedCompartmentID == "" {
			m.savedCompartmentID = current.TenancyOCID
		}
		m.savedRegion = current.Region
	}
	for _, it := range items {
		if _, ok := it.(sectionItem); ok {
			m.managedContextMenu = true
			break
		}
		if _, ok := it.(separatorItem); ok {
			m.managedContextMenu = true
			break
		}
	}
	m.refreshDelegates()
	m.refreshContextMenuItems()
	m.applyStartMode(startMode)
	m.resizeListsForViewport()
	return m
}

func (m *tuiModel) resizeListsForViewport() {
	if m.width <= 0 {
		m.width = 80
	}
	if m.height <= 0 {
		m.height = 20
	}

	panelInnerWidth := m.width - 4 // panel border + horizontal padding
	if panelInnerWidth < 24 {
		panelInnerWidth = 24
	}

	// Reserve lines for top chrome so list content doesn't push header/tabs out of view.
	// header(1) + tabs(1) + panel border(2) + meta(1)
	reserved := 5
	if !m.ultraCompact && !m.helpVisible && !m.shouldInlineHotkeys() {
		// one extra row for condensed key hints when not inlined with state
		reserved++
	}
	if m.status != "" {
		reserved++
	}
	if m.mode == "compartments" && m.crumb != "" {
		reserved++
	}

	panelInnerHeight := m.height - reserved
	if panelInnerHeight < 4 {
		panelInnerHeight = 4
	}
	m.panelInnerHeight = panelInnerHeight

	m.list.SetSize(panelInnerWidth, panelInnerHeight)
	m.tenancies.SetSize(panelInnerWidth, panelInnerHeight)
	m.comps.SetSize(panelInnerWidth, panelInnerHeight)
	m.regions.SetSize(panelInnerWidth, panelInnerHeight)
}

func (m *tuiModel) refreshDelegates() {
	m.list.SetDelegate(newContextDelegate(&m.pendingContextName, &m.savedContextName, m.ultraCompact || !m.isModeVerbose("contexts")))
	m.tenancies.SetDelegate(newTenancyDelegate(&m.pendingTenancyOCID, &m.savedTenancyOCID, m.ultraCompact || !m.isModeVerbose("tenancies")))
	m.comps.SetDelegate(newCompDelegate(&m.pendingSelectionID, &m.savedCompartmentID, m.ultraCompact || !m.isModeVerbose("compartments")))
	m.regions.SetDelegate(newRegionDelegate(&m.pendingRegion, &m.savedRegion, m.ultraCompact || !m.isModeVerbose("regions")))
	m.applyDensityMode()
}

func (m *tuiModel) applyDensityMode() {
	// Keep list titles hidden; tabs/header already provide context and this saves vertical space.
	m.list.Title = ""
	m.tenancies.Title = ""
	m.comps.Title = ""
	m.regions.Title = ""
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

func (m tuiModel) ensureActiveContext() (tuiModel, bool) {
	if m.ctxItem != (contextItem{}) {
		return m, true
	}
	if item, ok := m.list.SelectedItem().(contextItem); ok {
		m.ctxItem = item
		return m, true
	}
	if ctx, ok := selectInitialContext(m.list.Items(), m.cfg.CurrentContext); ok {
		m.ctxItem = ctx
		return m, true
	}
	return m, false
}

func (m tuiModel) switchToMenu(target string) (tuiModel, tea.Cmd, bool) {
	switch target {
	case "contexts":
		m.mode = "contexts"
		m.status = ""
		m.crumb = ""
		return m, nil, true
	case "tenancies":
		if len(m.tenancies.Items()) == 0 {
			return m, nil, false
		}
		m.mode = "tenancies"
		m.status = "Select tenancy (Enter to use a profile and open root)"
		return m, nil, true
	case "compartments":
		var ok bool
		m, ok = m.ensureActiveContext()
		if !ok {
			return m, nil, false
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
		return m, m.loadCompsCmd(parent), true
	case "regions":
		var ok bool
		m, ok = m.ensureActiveContext()
		if !ok {
			return m, nil, false
		}
		m.mode = "regions"
		m.status = "Loading regions..."
		if cached, exists := m.regionCache[m.ctxItem.Name]; exists {
			m.regions.SetItems(toRegionList(cached))
			m.regions.Select(0)
			m.status = "Select region (Space to stage, Ctrl+S to save)"
			return m, nil, true
		}
		return m, m.loadRegionsCmd(m.ctxItem), true
	default:
		return m, nil, false
	}
}

func (m tuiModel) cycleMenu(forward bool) (tea.Model, tea.Cmd) {
	order := []string{"contexts", "tenancies", "compartments", "regions"}
	cur := 0
	for i, mode := range order {
		if mode == m.mode {
			cur = i
			break
		}
	}
	for step := 1; step <= len(order); step++ {
		next := (cur + step) % len(order)
		if !forward {
			next = (cur - step + len(order)) % len(order)
		}
		nextMode := order[next]
		nm, cmd, ok := m.switchToMenu(nextMode)
		if ok {
			return nm, cmd
		}
	}
	return m, nil
}

func (m tuiModel) Init() tea.Cmd {
	return m.initCmd
}

func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	m.refreshDelegates()
	m.resizeListsForViewport()
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resizeListsForViewport()
	case tea.KeyMsg:
		// In wide mode, navigate active list as a grid with arrows or vim keys.
		if m.shouldUseGridLayout() && m.moveActiveSelectionGrid(msg.String()) {
			return m, nil
		}

		// While actively typing a filter, defer all keys to the list component.
		if m.activeListFilterState() == list.Filtering {
			return m.updateActiveList(msg)
		}

		switch msg.String() {
		case "tab":
			return m.cycleMenu(true)
		case "shift+tab":
			return m.cycleMenu(false)
		case "enter", "right":
			if m.activeListFilterState() == list.FilterApplied {
				// Don't drill on enter while a filter is applied; use '/' to edit filter or Esc to clear.
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
					if m.pendingContextName == item.Name {
						m.pendingContextName = ""
						m.status = fmt.Sprintf("Context %s unstaged", item.Name)
						return m, nil
					}
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
					if m.pendingTenancyOCID == item.TenancyOCID {
						m.pendingTenancyOCID = ""
						m.status = fmt.Sprintf("Tenancy %s unstaged", abbreviateOCID(item.TenancyOCID))
						return m, nil
					}
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
					if m.pendingSelectionID == item.oc.ID {
						m.pendingSelectionID = ""
						m.pendingSelectionNm = ""
						m.status = fmt.Sprintf("Compartment %s unstaged", item.oc.Name)
						return m, nil
					}
					m.pendingSelectionID = item.oc.ID
					m.pendingSelectionNm = item.oc.Name
					m.status = fmt.Sprintf("Selected %s (pending save; Enter/right to drill, Ctrl+S/q to save)", item.oc.Name)
				}
				return m, nil
			}
			if m.mode == "regions" {
				if item, ok := m.regions.SelectedItem().(regionItem); ok {
					if m.pendingRegion == item.name {
						m.pendingRegion = ""
						m.status = fmt.Sprintf("Region %s unstaged", item.name)
						return m, nil
					}
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
		case "esc":
			if m.activeListFilterState() == list.FilterApplied {
				m.clearActiveAppliedFilter()
				m.status = "Filter cleared"
				return m, nil
			}
			return m, tea.Quit
		case "ctrl+c":
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
		case "?":
			m.helpVisible = !m.helpVisible
			if m.helpVisible {
				m.status = "Keybindings help: ON"
			} else {
				m.status = "Keybindings help: OFF"
			}
			return m, nil
		case "v":
			if m.layoutOverride == "matrix" || m.shouldUseGridLayout() {
				m.setModeVerbose(m.mode, true)
				m.layoutOverride = "list"
				if m.mode == "contexts" {
					m.refreshContextMenuItems()
				}
				m.refreshDelegates()
				m.resizeListsForViewport()
				m.status = fmt.Sprintf("Verbose ON for %s (list)", m.mode)
				return m, nil
			}
			next := !m.isModeVerbose(m.mode)
			m.setModeVerbose(m.mode, next)
			if m.mode == "contexts" {
				m.refreshContextMenuItems()
			}
			m.refreshDelegates()
			m.resizeListsForViewport()
			m.status = fmt.Sprintf("Verbose %s for %s (session)", onOff(next), m.mode)
			return m, nil
		case "m":
			if m.layoutOverride == "matrix" || m.shouldUseGridLayout() {
				m.layoutOverride = "list"
				m.status = "Layout list (session)"
			} else {
				m.layoutOverride = "matrix"
				m.setModeVerbose(m.mode, false)
				if m.mode == "contexts" {
					m.refreshContextMenuItems()
				}
				m.refreshDelegates()
				m.resizeListsForViewport()
				m.status = "Layout matrix + verbose OFF (session)"
				return m, nil
			}
			m.refreshDelegates()
			m.resizeListsForViewport()
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
		if km, ok := msg.(tea.KeyMsg); ok && isVerticalNavKey(km.String()) {
			m.skipNonContextRows(navDirection(km.String()))
		}
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
		return m.theme.panel.Render(lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true).Render(fmt.Sprintf("error: %v", m.err)))
	}
	if m.finalized {
		return fmt.Sprintf("Selected context %s with compartment %s\n", m.ctxItem.Name, m.parentID)
	}
	panelContent := m.activeListView()
	if m.mode == "compartments" && m.crumb != "" {
		panelContent = m.theme.statusMuted.Render(m.crumb) + "\n" + panelContent
	}

	lines := []string{
		m.renderHeader(),
		m.renderTabs(),
		m.theme.panel.Render(panelContent),
	}

	if !m.ultraCompact && m.helpVisible {
		lines = append(lines, m.theme.panel.Render(m.renderHelpPanel()))
	}

	if m.shouldInlineHotkeys() {
		lines = append(lines, m.renderMetaLineWithHotkeys())
	} else {
		if !m.ultraCompact && !m.helpVisible {
			lines = append(lines, m.theme.instructions.Render(primaryHotkeys(m.width > 0 && m.width < 72)))
		}
		lines = append(lines, m.renderMetaLine())
	}

	if m.status != "" {
		lines = append(lines, m.renderStatusLine())
	}

	return strings.Join(lines, "\n")
}

func (m tuiModel) renderStatusLine() string {
	s := m.status
	lower := strings.ToLower(s)
	switch {
	case strings.Contains(lower, "error"), strings.Contains(lower, "failed"):
		return m.theme.statusErr.Render(s)
	case strings.Contains(lower, "loading"):
		return m.theme.statusWarn.Render(s)
	case strings.Contains(lower, "select"):
		return m.theme.statusInfo.Render(s)
	default:
		return m.theme.status.Render(s)
	}
}

func (m tuiModel) activeListView() string {
	if m.shouldUseGridLayout() {
		return m.renderActiveGrid()
	}
	switch m.mode {
	case "contexts":
		return m.list.View()
	case "tenancies":
		return m.tenancies.View()
	case "regions":
		return m.regions.View()
	default:
		return m.comps.View()
	}
}

func (m tuiModel) activeListModel() list.Model {
	switch m.mode {
	case "contexts":
		return m.list
	case "tenancies":
		return m.tenancies
	case "regions":
		return m.regions
	default:
		return m.comps
	}
}

func (m tuiModel) activeListFilterState() list.FilterState {
	switch m.mode {
	case "contexts":
		return m.list.FilterState()
	case "tenancies":
		return m.tenancies.FilterState()
	case "regions":
		return m.regions.FilterState()
	default:
		return m.comps.FilterState()
	}
}

func (m *tuiModel) clearActiveAppliedFilter() {
	l := m.activeListModel()
	l.SetFilterText("")
	l.SetFilterState(list.Unfiltered)
	m.setActiveListModel(l)
}

func (m tuiModel) updateActiveList(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	l := m.activeListModel()
	l, cmd = l.Update(msg)
	m.setActiveListModel(l)
	if km, ok := msg.(tea.KeyMsg); ok && m.mode == "contexts" && isVerticalNavKey(km.String()) {
		m.skipNonContextRows(navDirection(km.String()))
	}
	return m, cmd
}

func (m *tuiModel) setActiveListModel(l list.Model) {
	switch m.mode {
	case "contexts":
		m.list = l
	case "tenancies":
		m.tenancies = l
	case "regions":
		m.regions = l
	default:
		m.comps = l
	}
}

func isVerticalNavKey(key string) bool {
	switch key {
	case "up", "k", "ctrl+p", "pgup", "down", "j", "ctrl+n", "pgdown":
		return true
	default:
		return false
	}
}

func navDirection(key string) int {
	switch key {
	case "up", "k", "ctrl+p", "pgup":
		return -1
	default:
		return 1
	}
}

func (m *tuiModel) skipNonContextRows(dir int) {
	items := m.list.Items()
	if len(items) == 0 {
		return
	}
	idx := m.list.Index()
	if idx < 0 || idx >= len(items) {
		return
	}
	if _, ok := items[idx].(contextItem); ok {
		return
	}

	// First try moving in the user's requested direction.
	for i := idx + dir; i >= 0 && i < len(items); i += dir {
		if _, ok := items[i].(contextItem); ok {
			m.list.Select(i)
			return
		}
	}
	// Fallback to the opposite direction if we hit an edge.
	for i := idx - dir; i >= 0 && i < len(items); i -= dir {
		if _, ok := items[i].(contextItem); ok {
			m.list.Select(i)
			return
		}
	}
}

func (m tuiModel) isFilteringActive() bool {
	switch m.mode {
	case "contexts":
		return m.list.FilterState() != list.Unfiltered
	case "tenancies":
		return m.tenancies.FilterState() != list.Unfiltered
	case "regions":
		return m.regions.FilterState() != list.Unfiltered
	default:
		return m.comps.FilterState() != list.Unfiltered
	}
}

func (m tuiModel) isModeVerbose(mode string) bool {
	switch mode {
	case "contexts":
		return m.prefs.VerboseContexts
	case "tenancies":
		return m.prefs.VerboseTenancies
	case "regions":
		return m.prefs.VerboseRegions
	default:
		return m.prefs.VerboseCompartments
	}
}

func (m *tuiModel) setModeVerbose(mode string, v bool) {
	switch mode {
	case "contexts":
		m.prefs.VerboseContexts = v
	case "tenancies":
		m.prefs.VerboseTenancies = v
	case "regions":
		m.prefs.VerboseRegions = v
	default:
		m.prefs.VerboseCompartments = v
	}
}

func onOff(v bool) string {
	if v {
		return "ON"
	}
	return "OFF"
}

func (m tuiModel) shouldUseGridLayout() bool {
	if m.layoutOverride == "matrix" {
		return m.gridAllowedInCurrentState()
	}
	if m.layoutOverride == "list" {
		return false
	}
	return m.effectiveGridLayout()
}

func (m tuiModel) effectiveGridLayout() bool {
	if !m.gridAllowedInCurrentState() {
		return false
	}
	if m.isModeVerbose(m.mode) {
		return false
	}
	return m.panelInnerHeight >= 3
}

func (m tuiModel) gridAllowedInCurrentState() bool {
	if m.ultraCompact || m.helpVisible || m.width < 96 || m.isFilteringActive() {
		return false
	}
	return len(m.activeGridItems()) > 0
}

func (m tuiModel) gridColumnsForCount(count int) int {
	if count <= 0 {
		return 1
	}
	colWidth := 32
	cols := m.width / colWidth
	if cols < 2 {
		cols = 2
	}
	if cols > 4 {
		cols = 4
	}
	if cols > count {
		cols = count
	}
	return cols
}

func (m *tuiModel) moveActiveSelectionGrid(key string) bool {
	mapIdx := m.activeGridIndexMap()
	items := m.activeGridItems()
	var delta int
	switch key {
	case "left", "h":
		delta = -1
	case "right", "l":
		delta = 1
	case "up", "k":
		delta = -m.gridColumnsForCount(len(items))
	case "down", "j":
		delta = m.gridColumnsForCount(len(items))
	default:
		return false
	}

	l := m.activeListModel()
	if len(items) == 0 || len(mapIdx) == 0 {
		return false
	}
	curList := l.Index()
	pos := 0
	for i, li := range mapIdx {
		if li == curList {
			pos = i
			break
		}
	}
	nextPos := pos + delta
	if nextPos < 0 || nextPos >= len(items) {
		return true
	}
	l.Select(mapIdx[nextPos])
	m.setActiveListModel(l)
	return true
}

func (m tuiModel) renderActiveGrid() string {
	l := m.activeListModel()
	idxMap := m.activeGridIndexMap()
	items := m.activeGridItems()
	if len(items) == 0 || len(idxMap) == 0 {
		return ""
	}

	cols := m.gridColumnsForCount(len(items))
	cellW := (m.width - 8) / cols
	if cellW < 18 {
		cellW = 18
	}
	totalRows := (len(items) + cols - 1) / cols
	visibleRows := m.panelInnerHeight
	if visibleRows < 1 {
		visibleRows = 1
	}

	selectedPos := 0
	for i, li := range idxMap {
		if li == l.Index() {
			selectedPos = i
			break
		}
	}
	selectedRow := selectedPos / cols

	// If we have profile+context groups and enough vertical room, render as stacked matrices.
	if m.mode == "contexts" {
		split := 0
		for i, it := range items {
			if ci, ok := it.(contextItem); ok && ci.fromSaved {
				split = i
				break
			}
		}
		if split > 0 && split < len(items) {
			rowsTop := (split + cols - 1) / cols
			rowsBottom := (len(items) - split + cols - 1) / cols
			if rowsTop+rowsBottom+1 <= visibleRows {
				top := m.renderGridRows(items[:split], cols, cellW, selectedPos, 0, rowsTop)
				bottom := m.renderGridRows(items[split:], cols, cellW, selectedPos, split, rowsBottom)
				divider := lipgloss.NewStyle().Foreground(panelColor).Render(strings.Repeat("─", max(12, m.width-8)))
				return strings.Join(append(append(top, divider), bottom...), "\n")
			}
		}
	}

	startRow := 0
	if totalRows > visibleRows {
		startRow = selectedRow - (visibleRows / 2)
		if startRow < 0 {
			startRow = 0
		}
		maxStart := totalRows - visibleRows
		if startRow > maxStart {
			startRow = maxStart
		}
	}
	endRow := startRow + visibleRows
	if endRow > totalRows {
		endRow = totalRows
	}

	lines := m.renderGridRows(items, cols, cellW, selectedPos, 0, endRow-startRow)
	return strings.Join(lines, "\n")
}

func (m tuiModel) activeGridItems() []list.Item {
	l := m.activeListModel()
	base := l.Items()
	out := make([]list.Item, 0, len(base))
	for _, it := range base {
		if _, sep := it.(separatorItem); sep {
			continue
		}
		if _, sec := it.(sectionItem); sec {
			continue
		}
		out = append(out, it)
	}
	return out
}

func (m tuiModel) activeGridIndexMap() []int {
	l := m.activeListModel()
	base := l.Items()
	out := make([]int, 0, len(base))
	for i, it := range base {
		if _, sep := it.(separatorItem); sep {
			continue
		}
		if _, sec := it.(sectionItem); sec {
			continue
		}
		out = append(out, i)
	}
	return out
}

func (m tuiModel) renderGridRows(items []list.Item, cols, cellW, selectedPos, offset, rows int) []string {
	lines := make([]string, 0, rows)
	totalRows := (len(items) + cols - 1) / cols
	if rows > totalRows {
		rows = totalRows
	}
	for row := 0; row < rows; row++ {
		i := row * cols
		cells := make([]string, 0, cols)
		for c := 0; c < cols; c++ {
			idx := i + c
			if idx >= len(items) {
				cells = append(cells, lipgloss.NewStyle().Width(cellW).Render(""))
				continue
			}
			title := itemTitle(items[idx])
			staged := m.isStagedItem(items[idx])
			if staged {
				if m.ultraCompact {
					title = "[*] " + title
				} else {
					title = title + " " + m.theme.gridStaged.Render("●")
				}
			}
			globalPos := offset + idx
			if globalPos == selectedPos {
				cells = append(cells, m.theme.gridSelected.Width(cellW).Render(title))
			} else {
				cells = append(cells, m.theme.gridCell.Width(cellW).Render(title))
			}
		}
		lines = append(lines, lipgloss.JoinHorizontal(lipgloss.Top, cells...))
	}
	return lines
}

func (m tuiModel) isStagedItem(item list.Item) bool {
	switch m.mode {
	case "contexts":
		if ci, ok := item.(contextItem); ok {
			return m.pendingContextName != "" && ci.Name == m.pendingContextName
		}
	case "tenancies":
		if ti, ok := item.(tenancyItem); ok {
			return m.pendingTenancyOCID != "" && ti.TenancyOCID == m.pendingTenancyOCID
		}
	case "regions":
		if ri, ok := item.(regionItem); ok {
			return m.pendingRegion != "" && ri.name == m.pendingRegion
		}
	default:
		if ci, ok := item.(compItem); ok {
			return m.pendingSelectionID != "" && ci.oc.ID == m.pendingSelectionID
		}
	}
	return false
}

func (m tuiModel) renderHeader() string {
	mode := strings.ToUpper(displayModeName(m.mode))
	return lipgloss.JoinHorizontal(
		lipgloss.Top,
		m.theme.headerTitle.Render("OCI Context"),
		" ",
		m.theme.headerSubtle.Render("• "+mode),
	)
}

func (m tuiModel) renderTabs() string {
	compact := m.width > 0 && m.width < 64
	labels := []struct {
		mode  string
		label string
	}{
		{mode: "contexts", label: "Profiles"},
		{mode: "tenancies", label: "Tenancies"},
		{mode: "compartments", label: "Compartments"},
		{mode: "regions", label: "Regions"},
	}
	if compact {
		labels = []struct {
			mode  string
			label string
		}{
			{mode: "contexts", label: "Prof"},
			{mode: "tenancies", label: "Ten"},
			{mode: "compartments", label: "Comp"},
			{mode: "regions", label: "Reg"},
		}
	}

	rendered := make([]string, 0, len(labels))
	for _, tab := range labels {
		label := tab.label
		if tab.mode == m.mode {
			rendered = append(rendered, m.theme.tabActive.Render(label))
			continue
		}
		rendered = append(rendered, m.theme.tabInactive.Render(label))
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, rendered...)
}

func (m tuiModel) renderMetaLine() string {
	meta := compactMeta(m)
	if m.width > 0 && m.width < 64 {
		meta = compactMetaNarrow(m)
	}
	if !m.ultraCompact {
		return m.theme.metaBar.Render(lipgloss.JoinHorizontal(
			lipgloss.Top,
			m.theme.metaLabel.Render("state "),
			m.theme.metaValue.Render(meta),
		))
	}
	return m.theme.metaBar.Render(lipgloss.JoinHorizontal(
		lipgloss.Top,
		m.theme.ultraBadge.Render("[ULTRA] "),
		m.theme.metaValue.Render(meta),
	))
}

func (m tuiModel) renderMetaLineWithHotkeys() string {
	meta := inlineStateSummary(m)
	hotkeys := primaryHotkeys(m.width > 0 && m.width < 90)
	return m.theme.metaBar.Render(lipgloss.JoinHorizontal(
		lipgloss.Top,
		m.theme.metaLabel.Render("state "),
		m.theme.metaValue.Render(meta),
		m.theme.metaLabel.Render("  keys "),
		m.theme.instructions.Render(hotkeys),
	))
}

func (m tuiModel) shouldInlineHotkeys() bool {
	if m.ultraCompact || m.helpVisible {
		return false
	}
	// Keep one-line state+keys only when there is room to avoid wrapping noise.
	return m.width >= 116
}

func primaryHotkeys(compact bool) string {
	if compact {
		return "enter/backspace drill/up • space stage • / filter • v verbose • m matrix • q save • ? help"
	}
	return "enter/backspace drill/up • space stage • / filter • v verbose • m matrix • q save • ? help"
}

func inlineStateSummary(m tuiModel) string {
	current := m.ctxItem.Name
	if current == "" {
		current = m.cfg.CurrentContext
	}
	if current == "" {
		current = "-"
	}
	layout := "list"
	if m.shouldUseGridLayout() {
		layout = "matrix"
	}
	detail := "compact"
	if m.isModeVerbose(m.mode) {
		detail = "verbose"
	}
	return fmt.Sprintf("mode:%s | current:%s | layout:%s | detail:%s", displayModeName(m.mode), current, layout, detail)
}

func displayModeName(mode string) string {
	if mode == "contexts" {
		return "profiles"
	}
	return mode
}

func (m tuiModel) renderHelpPanel() string {
	lines := []string{
		"Keys",
		"Enter/right: drill or apply",
		"Space: stage selection",
		"Ctrl+S or q: save and quit",
		"Esc or Ctrl+C: quit without saving",
		"/: filter current list",
		"v: toggle verbose view for current mode",
		"m: toggle matrix layout for current session",
		"Backspace/delete: go up/back (when not filtering)",
		"?: toggle this help panel",
		"",
		"Mode Navigation",
		"contexts: r regions • c compartments • t tenancies",
		"submenus: R regions • C compartments • T tenancies • P profiles",
	}
	if m.width > 0 && m.width < 72 {
		lines = []string{
			"Keys: enter drill, space stage, q save, esc quit, / filter, ? help",
			"Switch: r/c/t in contexts, R/C/T/P in submenus",
		}
	}
	return strings.Join(lines, "\n")
}

func compactMetaNarrow(m tuiModel) string {
	staged := "-"
	if m.pendingContextName != "" {
		staged = "ctx:" + m.pendingContextName
	}
	if m.pendingTenancyOCID != "" {
		staged = "ten:" + abbreviateOCID(m.pendingTenancyOCID)
	}
	if m.pendingSelectionID != "" {
		staged = "comp:" + abbreviateOCID(m.pendingSelectionID)
	}
	if m.pendingRegion != "" {
		staged = "reg:" + m.pendingRegion
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
	return fmt.Sprintf("m:%s c:%s s:%s f:%s", m.mode, current, staged, filter)
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
			if m.pendingSelectionID != "" && prevCtxItem.Name == item.Name {
				parent = m.pendingSelectionID
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
		if m.pendingSelectionID != "" {
			m.parentID = m.pendingSelectionID
			if m.pendingSelectionNm != "" {
				m.parentCrumb = m.pendingSelectionNm
			}
		}
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
	// persist selection (compartment + region if set)
	m.ctxItem.CompartmentOCID = m.parentID
	m.maybeDeriveContextName()
	m.selected = m.ctxItem.Name
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
	if err := syncOCIDefaultsForCurrent(m.cfg); err != nil {
		m.err = err
		return m, tea.Quit
	}
	return m, tea.Quit
}

func (m *tuiModel) maybeDeriveContextName() {
	profileName := strings.TrimSpace(m.ctxItem.Profile)
	if profileName == "" {
		return
	}
	p, ok := m.profiles[profileName]
	if !ok {
		return
	}
	baseTenancy := p.Tenancy
	baseRegion := p.Region
	baseComp := p.Tenancy

	if m.ctxItem.TenancyOCID == baseTenancy && m.ctxItem.Region == baseRegion && m.ctxItem.CompartmentOCID == baseComp {
		if m.ctxItem.Name == "" {
			m.ctxItem.Name = profileName
		}
		return
	}

	name := profileName
	if m.ctxItem.Region != "" {
		name += "@" + m.ctxItem.Region
	}
	if m.ctxItem.TenancyOCID != "" && m.ctxItem.TenancyOCID != baseTenancy {
		name += ":" + abbreviateOCID(m.ctxItem.TenancyOCID)
	}
	if m.ctxItem.CompartmentOCID != "" && m.ctxItem.CompartmentOCID != m.ctxItem.TenancyOCID {
		name += "/" + abbreviateOCID(m.ctxItem.CompartmentOCID)
	}
	m.ctxItem.Name = name
}

func toList(items []compItem) []list.Item {
	out := make([]list.Item, len(items))
	for i, it := range items {
		out[i] = it
	}
	return out
}
