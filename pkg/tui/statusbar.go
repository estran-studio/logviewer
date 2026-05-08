// Package tui provides the terminal user interface components.
package tui

import (
	"fmt"
	"strings"

	"github.com/estran-studio/logviewer/pkg/log/client"
	"github.com/charmbracelet/lipgloss"
)

// StatusBarStyles defines the styles for the status bar
type StatusBarStyles struct {
	Container      lipgloss.Style
	Label          lipgloss.Style
	Value          lipgloss.Style
	Separator      lipgloss.Style
	FollowActive   lipgloss.Style
	FollowInactive lipgloss.Style
	PaginationMore lipgloss.Style
	Loading        lipgloss.Style
}

// DefaultStatusBarStyles returns the default styles for the status bar
func DefaultStatusBarStyles() StatusBarStyles {
	return StatusBarStyles{
		Container: lipgloss.NewStyle().
			BorderStyle(lipgloss.NormalBorder()).
			BorderTop(true).
			BorderBottom(true).
			BorderForeground(ColorBorder).
			Padding(0, 1),
		Label: lipgloss.NewStyle().
			Foreground(ColorMuted),
		Value: lipgloss.NewStyle().
			Foreground(ColorText),
		Separator: lipgloss.NewStyle().
			Foreground(ColorMuted),
		FollowActive: lipgloss.NewStyle().
			Foreground(ColorSuccess).
			Bold(true),
		FollowInactive: lipgloss.NewStyle().
			Foreground(ColorMuted),
		PaginationMore: lipgloss.NewStyle().
			Foreground(ColorWarning),
		Loading: lipgloss.NewStyle().
			Foreground(ColorPrimary).
			Bold(true),
	}
}

// StatusBar displays search metadata between viewport and search input
type StatusBar struct {
	Width  int
	Styles StatusBarStyles

	// Data to display
	TimeRange      *client.SearchRange
	Size           int
	HasMore        bool
	NextPageToken  string
	FollowMode     bool
	RefreshRate    string
	EntryCount     int
	FilteredCount  int // Number of entries after client-side filtering
	CursorPosition int
	ContextID      string
	Loading        bool   // Whether a request is in progress
	LoadingMore    bool   // Whether pagination is loading more entries
	Message        string // Temporary status message
}

// NewStatusBar creates a new status bar with default styles
func NewStatusBar() StatusBar {
	return StatusBar{
		Width:  80,
		Styles: DefaultStatusBarStyles(),
	}
}

// SetMessage sets a temporary status message
func (s *StatusBar) SetMessage(message string) {
	s.Message = message
}

// ClearMessage clears the temporary status message
func (s *StatusBar) ClearMessage() {
	s.Message = ""
}

// UpdateFromTab updates the status bar data from a tab
func (s *StatusBar) UpdateFromTab(tab *Tab) {
	if tab == nil {
		return
	}

	s.Loading = tab.Loading
	s.LoadingMore = tab.LoadingMore
	s.EntryCount = len(tab.Entries)
	s.CursorPosition = tab.Cursor
	s.ContextID = tab.ContextID

	// First, get values from the result (server response)
	if tab.Result != nil {
		search := tab.Result.GetSearch()
		if search != nil {
			s.TimeRange = &search.Range
			s.Size = search.Size.Value
			s.FollowMode = search.Follow
			if search.Refresh.Duration.Set {
				s.RefreshRate = search.Refresh.Duration.Value
			}
		}

		pagination := tab.Result.GetPaginationInfo()
		if pagination != nil {
			s.HasMore = pagination.HasMore
			s.NextPageToken = pagination.NextPageToken
		}
	}

	// Override with local tab.Search values (from chips) if set
	if tab.Search != nil {
		localRange := &tab.Search.Range
		if localRange.Last.Set || localRange.Gte.Set || localRange.Lte.Set {
			s.TimeRange = localRange
		}
		if tab.Search.Size.Set {
			s.Size = tab.Search.Size.Value
		}
	}
}

// SetFilteredCount sets the number of entries after client-side filtering
func (s *StatusBar) SetFilteredCount(count int) {
	s.FilteredCount = count
}

// UpdateTimeRangeFromChips updates the time range display from search bar chips
func (s *StatusBar) UpdateTimeRangeFromChips(chips []Chip) {
	// Build time range from chips
	timeRange := &client.SearchRange{}
	hasTimeChip := false

	for _, chip := range chips {
		switch chip.Type {
		case ChipTypeTimeRange:
			hasTimeChip = true
			switch chip.Field {
			case "last":
				timeRange.Last.S(chip.Value)
			case "from":
				timeRange.Gte.S(chip.Value)
			case "to":
				timeRange.Lte.S(chip.Value)
			}
		case ChipTypeSize:
			// Also extract size from chips
			var sizeVal int
			if _, err := fmt.Sscanf(chip.Value, "%d", &sizeVal); err == nil && sizeVal > 0 {
				s.Size = sizeVal
			}
		}
	}

	if hasTimeChip {
		s.TimeRange = timeRange
	}
}

// View renders the status bar
func (s StatusBar) View() string {
	if s.Width < 20 {
		return ""
	}

	// Show temporary message if present
	if s.Message != "" {
		messageStyle := lipgloss.NewStyle().
			Background(ColorPrimary).
			Foreground(ColorText).
			Bold(true).
			Padding(0, 1)

		messageLine := messageStyle.Render(s.Message)
		// Render message prominently
		return lipgloss.NewStyle().
			Width(s.Width).
			Border(lipgloss.NormalBorder(), true, false).
			BorderForeground(ColorPrimary).
			Render(messageLine)
	}

	var line1Parts []string
	var line2Parts []string

	// Line 1: Time range and size
	if s.TimeRange != nil {
		if s.TimeRange.Last.Set && s.TimeRange.Last.Value != "" {
			line1Parts = append(line1Parts,
				s.Styles.Label.Render("Last: ")+s.Styles.Value.Render(s.TimeRange.Last.Value))
		} else {
			if s.TimeRange.Gte.Set && s.TimeRange.Gte.Value != "" {
				line1Parts = append(line1Parts,
					s.Styles.Label.Render("From: ")+s.Styles.Value.Render(s.TimeRange.Gte.Value))
			}
			if s.TimeRange.Lte.Set && s.TimeRange.Lte.Value != "" {
				line1Parts = append(line1Parts,
					s.Styles.Label.Render("To: ")+s.Styles.Value.Render(s.TimeRange.Lte.Value))
			}
		}
	}

	if s.Size > 0 {
		line1Parts = append(line1Parts,
			s.Styles.Label.Render("Size: ")+s.Styles.Value.Render(fmt.Sprintf("%d", s.Size)))
	}

	if s.ContextID != "" {
		line1Parts = append(line1Parts,
			s.Styles.Label.Render("Context: ")+s.Styles.Value.Render(s.ContextID))
	}

	// Line 2: Loading indicator, entries, pagination, follow mode, position
	if s.Loading {
		line2Parts = append(line2Parts, s.Styles.Loading.Render("⏳ Loading..."))
	} else if s.LoadingMore {
		line2Parts = append(line2Parts, s.Styles.Loading.Render("⏳ Loading more..."))
	}

	if s.FilteredCount > 0 && s.FilteredCount != s.EntryCount {
		line2Parts = append(line2Parts,
			s.Styles.Label.Render("Entries: ")+s.Styles.Value.Render(fmt.Sprintf("%d/%d", s.FilteredCount, s.EntryCount)))
	} else {
		line2Parts = append(line2Parts,
			s.Styles.Label.Render("Entries: ")+s.Styles.Value.Render(fmt.Sprintf("%d", s.EntryCount)))
	}

	if s.HasMore {
		line2Parts = append(line2Parts,
			s.Styles.PaginationMore.Render("[More available]"))
	}

	// Follow mode indicator
	if s.FollowMode {
		followText := "LIVE"
		if s.RefreshRate != "" {
			followText = fmt.Sprintf("LIVE (%s)", s.RefreshRate)
		}
		line2Parts = append(line2Parts, s.Styles.FollowActive.Render(followText))
	} else {
		line2Parts = append(line2Parts, s.Styles.FollowInactive.Render("Follow: OFF"))
	}

	// Current position
	displayCount := s.EntryCount
	if s.FilteredCount > 0 {
		displayCount = s.FilteredCount
	}
	if displayCount > 0 {
		line2Parts = append(line2Parts,
			s.Styles.Value.Render(fmt.Sprintf("Line %d/%d", s.CursorPosition+1, displayCount)))
	}

	// Build lines
	sep := s.Styles.Separator.Render(" | ")
	line1 := strings.Join(line1Parts, sep)
	line2 := strings.Join(line2Parts, sep)

	// Handle empty time range - show N/A
	if len(line1Parts) == 0 || (s.TimeRange == nil || (!s.TimeRange.Last.Set && !s.TimeRange.Gte.Set && !s.TimeRange.Lte.Set)) {
		line1Parts = append([]string{s.Styles.Label.Render("Time: ") + s.Styles.Value.Render("N/A")}, line1Parts...)
		line1 = strings.Join(line1Parts, sep)
	}

	content := lipgloss.JoinVertical(lipgloss.Left, line1, line2)

	return s.Styles.Container.Width(s.Width).Render(content)
}

// Height returns the height of the status bar in lines
func (s StatusBar) Height() int {
	return 2 // Two lines of content (plus borders handled by lipgloss)
}
