package vmproxy

import (
	"fmt"
	"io"
	"log"
	"net"
	"net/netip"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/timer"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish"
	bm "github.com/charmbracelet/wish/bubbletea"
	"github.com/digitalocean/go-libvirt"
	"tailscale.com/util/must"

	lm "github.com/charmbracelet/wish/logging"
)

const listHeight = 14

var (
	titleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFDF5")).
			Background(lipgloss.Color("#25A065")).
			Padding(0, 1)
	itemStyle         = lipgloss.NewStyle().PaddingLeft(4)
	selectedItemStyle = lipgloss.NewStyle().PaddingLeft(2).Foreground(lipgloss.Color("170"))
	paginationStyle   = list.DefaultStyles().PaginationStyle.PaddingLeft(4)
	helpStyle         = list.DefaultStyles().HelpStyle.PaddingLeft(4).PaddingBottom(1)
	quitTextStyle     = lipgloss.NewStyle().Margin(1, 0, 2, 4)
	errorMessageStyle = lipgloss.NewStyle().
				Foreground(lipgloss.AdaptiveColor{Light: "#B50475", Dark: "#B50475"}).
				Render
	statusMessageStyle = lipgloss.NewStyle().
				Foreground(lipgloss.AdaptiveColor{Light: "#04B575", Dark: "#04B575"}).
				Render
)

type SSHConfig struct {
	Domain     string // vm name
	SSHDir     string // ssh directory
	LibvirtLoc string // libvirt socket path or <ip addr>:<port>
}

type SSHServer struct {
	config *SSHConfig
}

func NewSSHServer(config *SSHConfig) *SSHServer {
	return &SSHServer{config: config}
}

func (s *SSHServer) Serve(ln net.Listener) error {
	keypath := filepath.Join(s.config.SSHDir, "term_info_ed25519")
	ws := must.Get(wish.NewServer(
		wish.WithAddress(fmt.Sprintf("%s:%d", ln.Addr().String(), 22)),
		wish.WithHostKeyPath(keypath),
		wish.WithMiddleware(
			bm.Middleware(s.wishMiddleware()),
			lm.Middleware(),
		),
	))

	log.Print("Starting SSH server on port 22")
	return ws.Serve(ln)
}

func (s *SSHServer) wishMiddleware() bm.Handler {
	return func(sess ssh.Session) (tea.Model, []tea.ProgramOption) {
		items := []list.Item{
			item("Start"),
			item("Stop"),
			item("Pause"),
			item("Resume"),
			item("Restart"),
			item("Force Stop"),
		}

		const defaultWidth = 20

		l := list.New(items, itemDelegate{}, defaultWidth, listHeight)
		l.Title = "VM Controls"
		l.SetShowStatusBar(false)
		l.SetFilteringEnabled(false)
		l.Styles.Title = titleStyle
		l.Styles.PaginationStyle = paginationStyle
		l.Styles.HelpStyle = helpStyle

		timeout := 2 * time.Second

		var c net.Conn
		if _, err := netip.ParseAddrPort(s.config.LibvirtLoc); err == nil {
			c = must.Get(net.DialTimeout("tcp", s.config.LibvirtLoc, timeout))
		} else {
			c = must.Get(net.DialTimeout("unix", s.config.LibvirtLoc, timeout))
		}

		m := model{
			list:  l,
			timer: timer.NewWithInterval(timeout, time.Second),
		}
		m.l = libvirt.New(c)
		must.Do(m.l.Connect())

		m.domain = must.Get(m.l.DomainLookupByName(s.config.Domain))

		return m, []tea.ProgramOption{tea.WithAltScreen()}
	}
}

type item string

func (i item) FilterValue() string { return "" }

type itemDelegate struct{}

func (d itemDelegate) Height() int                               { return 1 }
func (d itemDelegate) Spacing() int                              { return 0 }
func (d itemDelegate) Update(msg tea.Msg, m *list.Model) tea.Cmd { return nil }
func (d itemDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	i, ok := listItem.(item)
	if !ok {
		return
	}

	str := fmt.Sprintf("%d. %s", index+1, i)

	fn := itemStyle.Render
	if index == m.Index() {
		fn = func(s ...string) string {
			return selectedItemStyle.Render("> " + strings.Join(s, " "))
		}
	}

	fmt.Fprint(w, fn(str))
}

type model struct {
	list   list.Model
	timer  timer.Model
	l      *libvirt.Libvirt
	domain libvirt.Domain
}

func (m model) Init() tea.Cmd {
	m.timer.Init()
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.list.SetWidth(msg.Width)
		return m, nil

	case timer.TickMsg:
		var cmd tea.Cmd
		m.timer, cmd = m.timer.Update(msg)
		return m, cmd

	case timer.StartStopMsg:
		var cmd tea.Cmd
		m.timer, cmd = m.timer.Update(msg)
		return m, cmd

	case timer.TimeoutMsg:
		m.list.NewStatusMessage("")
		m.timer.Stop()
		return m, nil

	case tea.KeyMsg:
		switch keypress := msg.String(); keypress {
		case "ctrl+c":
			return m, tea.Quit

		case "esc":
			return m, tea.Quit

		case "enter":
			var err error
			var cmd tea.Cmd
			m.timer.Timeout = 2 * time.Second
			cmd = m.timer.Start()

			if m.l == nil {
				log.Fatal("libvirt is nil")
				log.Fatalf("domain is %+v", m.domain)
			}

			i, ok := m.list.SelectedItem().(item)
			if ok {
				switch i {
				case "Start":
					err = m.l.DomainCreate(m.domain)
				case "Stop":
					err = m.l.DomainShutdown(m.domain)
				case "Pause":
					err = m.l.DomainSuspend(m.domain)
				case "Resume":
					err = m.l.DomainResume(m.domain)
				case "Restart":
					err = m.l.DomainReboot(m.domain, libvirt.DomainRebootDefault)
				case "Force Stop":
					err = m.l.DomainDestroy(m.domain)
				}
				if err != nil {
					m.list.NewStatusMessage(errorMessageStyle(fmt.Errorf("%s failed with: %w", i, err).Error()))
				} else {
					m.list.NewStatusMessage(statusMessageStyle(fmt.Sprintf("%s successful", i)))
				}
			}
			return m, cmd
		}
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m model) View() string {
	return "\n" + m.list.View()
}
