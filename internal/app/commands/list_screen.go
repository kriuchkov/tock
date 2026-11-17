package commands

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kriuchkov/tock/internal/app/localization"
	"github.com/kriuchkov/tock/internal/config"
	"github.com/kriuchkov/tock/internal/core/ports"
	"github.com/kriuchkov/tock/internal/timeutil"
)

// listPeriodCount is the number of views of the list tab: day, week, month and year.
const listPeriodCount = 4

// listScreen is the list tab: the activities of a day, or the time per project in a week,
// a month or a year. tab switches between them, keeping the date in view.
type listScreen struct {
	service ports.ActivityResolver
	config  *config.Config
	tf      *timeutil.Formatter
	loc     *localization.Localizer
	period  listPeriod
	day     *listModel
	periods *listPeriodModel
	tabs    tabStrip
	size    *tea.WindowSizeMsg
}

func newListScreen(
	service ports.ActivityResolver,
	cfg *config.Config,
	tf *timeutil.Formatter,
	loc *localization.Localizer,
	period listPeriod,
) *listScreen {
	s := &listScreen{service: service, config: cfg, tf: tf, loc: loc, tabs: singleTab(loc.Text("tui.tab.list"))}
	s.show(period, time.Now())
	return s
}

// current is the model of the view shown.
func (s *listScreen) current() shellScreen {
	if s.period == periodDaily {
		return s.day
	}
	return s.periods
}

// date is the day in view: the day listed, or a day of the period summed up.
func (s *listScreen) date() time.Time {
	if s.period == periodDaily {
		return s.day.selectedDate
	}
	return s.periods.anchor
}

// show builds the view of period around date.
func (s *listScreen) show(period listPeriod, date time.Time) {
	s.period = period
	switch {
	case period == periodDaily && s.day != nil:
		// The day view keeps the activities it read; no need to read them all again.
		s.day.showDay(date)
	case period == periodDaily:
		model := newListModel(s.service, s.tf, s.loc, GetTheme(s.config.Theme), date)
		s.day = &model
	default:
		model := newListPeriodModel(s.service, s.config, s.loc, period, date)
		s.periods = &model
	}
	s.current().setTabs(s.tabs)
	if s.size != nil {
		s.current().Update(*s.size)
	}
}

func (s *listScreen) Init() tea.Cmd { return nil }

func (s *listScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.size = &msg
	case listLoaded:
		// The day view keeps the activities even while a period view is shown.
		if s.day != nil {
			s.day.Update(msg)
		}
		return s, nil
	case tea.KeyMsg:
		if s.modal() {
			break
		}
		switch msg.String() {
		case keyTab:
			s.show((s.period+1)%listPeriodCount, s.date())
			return s, nil
		case keyShiftTab:
			s.show((s.period+listPeriodCount-1)%listPeriodCount, s.date())
			return s, nil
		}
	}
	_, cmd := s.current().Update(msg)
	return s, cmd
}

func (s *listScreen) View() string { return s.current().View() }

func (s *listScreen) setTabs(tabs tabStrip) {
	s.tabs = tabs
	s.current().setTabs(tabs)
}

func (s *listScreen) modal() bool { return s.current().modal() }

func (s *listScreen) refresh() tea.Cmd { return s.current().refresh() }
