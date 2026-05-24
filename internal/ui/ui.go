package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/raj/speedtest/internal/engine"
)

// Run executes the speedtest with an animated TUI.
func Run() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m := newModel(ctx)
	p := tea.NewProgram(m, tea.WithAltScreen())
	finalModel, err := p.Run()
	if err != nil {
		return err
	}
	if fm, ok := finalModel.(*model); ok && fm.done {
		fmt.Print(fm.summary())
	}
	return nil
}

// ── palette ────────────────────────────────────────────────────────────────

var (
	cBg     = lipgloss.Color("#0B0F19")
	cInk    = lipgloss.Color("#E5E7EB")
	cDim    = lipgloss.Color("#6B7280")
	cFaint  = lipgloss.Color("#374151")
	cAccent = lipgloss.Color("#7AF7C9")
	cDown   = lipgloss.Color("#5CC8FF")
	cUp     = lipgloss.Color("#FF7AD9")
	cWarn   = lipgloss.Color("#F59E0B")
	cErr    = lipgloss.Color("#EF4444")
	cGood   = lipgloss.Color("#34D399")

	// gradient ramps for speed bars (low → high)
	dlRamp = []lipgloss.Color{"#1E3A5F", "#2E6BAE", "#3D9CDB", "#5CC8FF", "#9BE7FF"}
	upRamp = []lipgloss.Color{"#4A1D52", "#8E2E9C", "#C84EBF", "#FF7AD9", "#FFB3E9"}
)

var (
	dim   = lipgloss.NewStyle().Foreground(cDim)
	faint = lipgloss.NewStyle().Foreground(cFaint)
	ink   = lipgloss.NewStyle().Foreground(cInk)
	label = lipgloss.NewStyle().Foreground(cDim).Bold(true)
	val   = lipgloss.NewStyle().Foreground(cInk).Bold(true)

	brand = lipgloss.NewStyle().
		Foreground(cAccent).
		Bold(true)

	subtitle = lipgloss.NewStyle().
			Foreground(cDim).
			Italic(true)

	errStyle = lipgloss.NewStyle().Foreground(cErr).Bold(true)

	box = lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(cFaint).
		Padding(0, 2)

	boxAccent = box.BorderForeground(cAccent)

	bigDown = lipgloss.NewStyle().Foreground(cDown).Bold(true)
	bigUp   = lipgloss.NewStyle().Foreground(cUp).Bold(true)

	chipPend = lipgloss.NewStyle().Foreground(cDim).Background(cBg).Padding(0, 1)
	chipAct  = lipgloss.NewStyle().Foreground(cBg).Background(cAccent).Bold(true).Padding(0, 1)
	chipDone = lipgloss.NewStyle().Foreground(cGood).Padding(0, 1)
)

// ── messages ───────────────────────────────────────────────────────────────

type updMsg engine.Update
type tickMsg time.Time
type doneMsg struct{}

// ── model ──────────────────────────────────────────────────────────────────

const histLen = 40

type model struct {
	ctx      context.Context
	ch       <-chan engine.Update
	frame    int
	startAt  time.Time
	stage    engine.Stage
	done     bool
	hasError bool
	err      error

	ip, isp, lat, lon                        string
	srvName, srvSponsor, srvCountry, srvHost string
	srvID                                    string
	distance                                 float64

	pings        []float64 // sample history
	ping, jitter float64

	dlHist  []float64
	ulHist  []float64
	dlMbps  float64
	ulMbps  float64
	dlPeak  float64
	ulPeak  float64

	termW, termH int
}

func newModel(ctx context.Context) *model {
	return &model{
		ctx:     ctx,
		ch:      engine.Run(ctx),
		startAt: time.Now(),
		stage:   engine.StageInit,
	}
}

func (m *model) Init() tea.Cmd { return tea.Batch(m.waitForUpdate(), tickEvery()) }

func tickEvery() tea.Cmd {
	return tea.Tick(80*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m *model) waitForUpdate() tea.Cmd {
	return func() tea.Msg {
		u, ok := <-m.ch
		if !ok {
			return doneMsg{}
		}
		return updMsg(u)
	}
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.termW, m.termH = msg.Width, msg.Height
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			return m, tea.Quit
		}
	case tickMsg:
		m.frame++
		if m.done {
			return m, nil
		}
		return m, tickEvery()
	case updMsg:
		m.applyUpdate(engine.Update(msg))
		if m.hasError {
			return m, tea.Quit
		}
		return m, m.waitForUpdate()
	case doneMsg:
		m.done = true
		return m, tea.Quit
	}
	return m, nil
}

func pushHist(h []float64, v float64) []float64 {
	h = append(h, v)
	if len(h) > histLen {
		h = h[len(h)-histLen:]
	}
	return h
}

func (m *model) applyUpdate(u engine.Update) {
	if u.Err != nil {
		m.hasError = true
		m.err = u.Err
		return
	}
	m.stage = u.Stage
	switch u.Stage {
	case engine.StageUser:
		m.ip, m.isp, m.lat, m.lon = u.IP, u.ISP, u.Lat, u.Lon
	case engine.StageServers:
		m.srvName, m.srvSponsor = u.ServerName, u.ServerSponsor
		m.srvCountry, m.srvHost, m.srvID = u.ServerCountry, u.ServerHost, u.ServerID
		m.distance = u.DistanceKM
	case engine.StagePing:
		if u.PingMS > 0 {
			m.ping = u.PingMS
			m.pings = pushHist(m.pings, u.PingMS)
		}
		if u.JitterMS > 0 {
			m.jitter = u.JitterMS
		}
	case engine.StageDownload:
		m.dlMbps = u.DownloadMB
		if u.DownloadMB > m.dlPeak {
			m.dlPeak = u.DownloadMB
		}
		m.dlHist = pushHist(m.dlHist, u.DownloadMB)
	case engine.StageUpload:
		m.ulMbps = u.UploadMB
		if u.UploadMB > m.ulPeak {
			m.ulPeak = u.UploadMB
		}
		m.ulHist = pushHist(m.ulHist, u.UploadMB)
	case engine.StageDone:
		m.ping, m.jitter = u.PingMS, u.JitterMS
		m.dlMbps, m.ulMbps = u.DownloadMB, u.UploadMB
		m.done = true
	}
}

// ── view ───────────────────────────────────────────────────────────────────

var spin = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
var pulse = []string{"●", "◉", "○", "◉"}

func (m *model) sp() string    { return spin[m.frame%len(spin)] }
func (m *model) pulse() string { return pulse[(m.frame/3)%len(pulse)] }

func (m *model) View() string {
	if m.hasError {
		return "\n" + errStyle.Render("✗ "+m.err.Error()) + "\n\n"
	}

	var b strings.Builder
	b.WriteString(m.header())
	b.WriteString("\n")
	b.WriteString(m.stagePipeline())
	b.WriteString("\n\n")

	// Top row: connection + server side by side
	top := lipgloss.JoinHorizontal(lipgloss.Top, m.connectionPanel(), "  ", m.serverPanel())
	b.WriteString(top)
	b.WriteString("\n\n")

	// Latency panel
	b.WriteString(m.latencyPanel())
	b.WriteString("\n\n")

	// Speed meters
	b.WriteString(m.meterPanel("DOWNLOAD", m.dlMbps, m.dlPeak, m.dlHist, dlRamp, bigDown, m.stage == engine.StageDownload))
	b.WriteString("\n")
	b.WriteString(m.meterPanel("UPLOAD  ", m.ulMbps, m.ulPeak, m.ulHist, upRamp, bigUp, m.stage == engine.StageUpload))
	b.WriteString("\n\n")
	b.WriteString(m.footer())
	b.WriteString("\n")
	content := b.String()
	const contentW = 82
	if m.termW > contentW {
		pad := strings.Repeat(" ", (m.termW-contentW)/2)
		lines := strings.Split(content, "\n")
		for i, ln := range lines {
			lines[i] = pad + ln
		}
		content = strings.Join(lines, "\n")
	}
	if m.termH > 0 {
		return lipgloss.PlaceVertical(m.termH, lipgloss.Center, content)
	}
	return content
}

// header — gradient brand mark
func (m *model) header() string {
	ramp := []lipgloss.Color{"#7AF7C9", "#5CC8FF", "#A78BFA", "#FF7AD9"}
	word := "SPEEDTEST"
	var out strings.Builder
	for i, r := range word {
		c := ramp[i%len(ramp)]
		out.WriteString(lipgloss.NewStyle().Foreground(c).Bold(true).Render(string(r)))
	}
	bar := lipgloss.NewStyle().Foreground(cFaint).Render(strings.Repeat("━", 30))
	return brand.Render("◢◤ ") + out.String() + brand.Render(" ◥◣  ") +
		subtitle.Render("Ookla protocol · animated") + "\n" + bar
}

// stage pipeline strip
func (m *model) stagePipeline() string {
	stages := []struct {
		s     engine.Stage
		label string
	}{
		{engine.StageUser, "you"},
		{engine.StageServers, "server"},
		{engine.StagePing, "ping"},
		{engine.StageDownload, "download"},
		{engine.StageUpload, "upload"},
	}
	rank := map[engine.Stage]int{
		engine.StageInit: 0, engine.StageUser: 1, engine.StageServers: 2,
		engine.StagePing: 3, engine.StageDownload: 4, engine.StageUpload: 5,
		engine.StageDone: 6,
	}
	cur := rank[m.stage]
	parts := []string{}
	for i, st := range stages {
		var chip string
		switch {
		case m.done || rank[st.s] < cur:
			chip = chipDone.Render("✓ " + st.label)
		case rank[st.s] == cur:
			chip = chipAct.Render(m.sp() + " " + st.label)
		default:
			chip = chipPend.Render("· " + st.label)
		}
		parts = append(parts, chip)
		if i < len(stages)-1 {
			parts = append(parts, dim.Render("─"))
		}
	}
	return lipgloss.JoinHorizontal(lipgloss.Center, parts...)
}

func (m *model) connectionPanel() string {
	title := lipgloss.NewStyle().Foreground(cAccent).Bold(true).Render("◉ YOU")
	if m.ip == "" {
		return box.Width(34).Render(title + "\n" + dim.Render(m.sp()+" resolving…"))
	}
	rows := []string{
		title,
		"",
		kv("IP", m.ip),
		kv("ISP", trunc(m.isp, 22)),
		kv("Coord", fmt.Sprintf("%s, %s", m.lat, m.lon)),
	}
	return box.Width(34).Render(strings.Join(rows, "\n"))
}

func (m *model) serverPanel() string {
	title := lipgloss.NewStyle().Foreground(cAccent).Bold(true).Render("◉ SERVER")
	if m.srvName == "" {
		return box.Width(46).Render(title + "\n" + dim.Render(m.sp()+" finding nearest…"))
	}
	const inner = 40
	const valW = inner - 8
	rows := []string{
		title,
		"",
		kv("Host", trunc(m.srvSponsor, valW)),
		kv("City", trunc(fmt.Sprintf("%s, %s", m.srvName, m.srvCountry), valW)),
		kvWrap("URL", m.srvHost, valW),
		kv("Dist", fmt.Sprintf("%.0f km  ·  id %s", m.distance, m.srvID)),
	}
	return box.Width(46).Render(strings.Join(rows, "\n"))
}

func (m *model) latencyPanel() string {
	title := lipgloss.NewStyle().Foreground(cAccent).Bold(true).Render("◉ LATENCY")
	pingTxt := "—"
	jitTxt := "—"
	if m.ping > 0 {
		pingTxt = pingColor(m.ping).Render(fmt.Sprintf("%6.2f ms", m.ping))
	}
	if m.jitter > 0 {
		jitTxt = pingColor(m.jitter).Render(fmt.Sprintf("%6.2f ms", m.jitter))
	}
	spark := renderSparkline(m.pings, 30, cAccent)
	row1 := fmt.Sprintf("  %s %s    %s %s",
		label.Render("Ping  "), pingTxt,
		label.Render("Jitter"), jitTxt)
	row2 := "  " + label.Render("History") + " " + spark
	return box.Width(82).Render(title + "\n\n" + row1 + "\n" + row2)
}

func (m *model) meterPanel(name string, mbps, peak float64, hist []float64, ramp []lipgloss.Color, st lipgloss.Style, active bool) string {
	var status string
	switch {
	case active:
		status = lipgloss.NewStyle().Foreground(cAccent).Render(m.pulse() + " LIVE")
	case mbps > 0:
		status = lipgloss.NewStyle().Foreground(cGood).Render("● DONE")
	default:
		status = faint.Render("○ wait")
	}
	titleLine := lipgloss.NewStyle().Foreground(cInk).Bold(true).Render(name) + "   " + status

	bigNum := "—"
	unit := dim.Render("Mbps")
	if mbps > 0 || active {
		bigNum = st.Render(fmt.Sprintf("%7.2f", mbps))
	}
	peakLine := faint.Render("peak —")
	if peak > 0 {
		peakLine = dim.Render(fmt.Sprintf("peak %.2f Mbps", peak))
	}

	scale := maxScale(peak)
	bar := gradientBar(mbps, scale, 60, ramp)
	scaleAxis := axisLine(scale, 60)
	spark := renderSparkline(hist, 60, ramp[len(ramp)-1])

	body := fmt.Sprintf("%s\n\n   %s  %s    %s\n\n   %s\n   %s\n   %s",
		titleLine, bigNum, unit, peakLine, bar, scaleAxis, spark)

	style := box
	if active {
		style = boxAccent
	}
	return style.Width(82).Render(body)
}

func (m *model) footer() string {
	left := dim.Render(fmt.Sprintf("⏱  %5.1fs", time.Since(m.startAt).Seconds()))
	mid := dim.Render("press q to quit")
	return "  " + left + "    " + mid
}

// ── helpers ────────────────────────────────────────────────────────────────

func kv(k, v string) string {
	return label.Width(7).Render(k) + " " + val.Render(v)
}

func kvWrap(k, v string, valW int) string {
	if valW <= 0 || len(v) <= valW {
		return kv(k, v)
	}
	indent := strings.Repeat(" ", 8)
	var lines []string
	first := true
	for len(v) > 0 {
		n := valW
		if n > len(v) {
			n = len(v)
		}
		chunk := v[:n]
		v = v[n:]
		if first {
			lines = append(lines, label.Width(7).Render(k)+" "+val.Render(chunk))
			first = false
		} else {
			lines = append(lines, indent+val.Render(chunk))
		}
	}
	return strings.Join(lines, "\n")
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func pingColor(ms float64) lipgloss.Style {
	switch {
	case ms < 20:
		return lipgloss.NewStyle().Foreground(cGood).Bold(true)
	case ms < 50:
		return lipgloss.NewStyle().Foreground(cAccent).Bold(true)
	case ms < 100:
		return lipgloss.NewStyle().Foreground(cWarn).Bold(true)
	default:
		return lipgloss.NewStyle().Foreground(cErr).Bold(true)
	}
}

// gradientBar — 1/8th-block resolution bar with color graded across ramp
func gradientBar(v, scale float64, width int, ramp []lipgloss.Color) string {
	if scale <= 0 || width <= 0 {
		return strings.Repeat(faint.Render("░"), width)
	}
	if v < 0 {
		v = 0
	}
	frac := v / scale
	if frac > 1 {
		frac = 1
	}
	total := frac * float64(width)
	full := int(total)
	rem := total - float64(full)
	partials := []string{" ", "▏", "▎", "▍", "▌", "▋", "▊", "▉"}
	pIdx := int(rem * 8)
	if pIdx > 7 {
		pIdx = 7
	}

	var out strings.Builder
	for i := 0; i < full; i++ {
		c := ramp[i*len(ramp)/width]
		out.WriteString(lipgloss.NewStyle().Foreground(c).Render("█"))
	}
	if full < width {
		c := ramp[full*len(ramp)/width]
		out.WriteString(lipgloss.NewStyle().Foreground(c).Render(partials[pIdx]))
		for i := full + 1; i < width; i++ {
			out.WriteString(faint.Render("·"))
		}
	}
	return out.String()
}

func axisLine(scale float64, width int) string {
	// 0 ────── scale/2 ────── scale
	left := fmt.Sprintf("0")
	mid := fmt.Sprintf("%.0f", scale/2)
	right := fmt.Sprintf("%.0f Mbps", scale)
	pad1 := width/2 - len(left) - len(mid)/2
	if pad1 < 1 {
		pad1 = 1
	}
	pad2 := width - width/2 - len(mid) - len(right)
	if pad2 < 1 {
		pad2 = 1
	}
	return faint.Render(left + strings.Repeat(" ", pad1) + mid + strings.Repeat(" ", pad2) + right)
}

func renderSparkline(values []float64, width int, c lipgloss.Color) string {
	if len(values) == 0 {
		return faint.Render(strings.Repeat("·", width))
	}
	// pick the last `width` values, pad left with faint dots
	vals := values
	if len(vals) > width {
		vals = vals[len(vals)-width:]
	}
	var max float64
	for _, v := range vals {
		if v > max {
			max = v
		}
	}
	if max <= 0 {
		return faint.Render(strings.Repeat("·", width))
	}
	blocks := []rune("▁▂▃▄▅▆▇█")
	leftPad := width - len(vals)
	var out strings.Builder
	if leftPad > 0 {
		out.WriteString(faint.Render(strings.Repeat("·", leftPad)))
	}
	st := lipgloss.NewStyle().Foreground(c)
	for _, v := range vals {
		idx := int((v / max) * float64(len(blocks)-1))
		if idx < 0 {
			idx = 0
		}
		if idx >= len(blocks) {
			idx = len(blocks) - 1
		}
		out.WriteString(st.Render(string(blocks[idx])))
	}
	return out.String()
}

func maxScale(peak float64) float64 {
	switch {
	case peak <= 0:
		return 100
	case peak < 25:
		return 25
	case peak < 100:
		return 100
	case peak < 250:
		return 250
	case peak < 500:
		return 500
	case peak < 1000:
		return 1000
	default:
		return peak * 1.1
	}
}

// ── final summary ──────────────────────────────────────────────────────────

func (m *model) summary() string {
	var b strings.Builder
	bar := lipgloss.NewStyle().Foreground(cFaint).Render(strings.Repeat("━", 60))
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, bar)
	fmt.Fprintln(&b, brand.Render("  ✓ SPEEDTEST RESULTS"))
	fmt.Fprintln(&b, bar)
	fmt.Fprintf(&b, "  %s  %s · %s\n", label.Render("Client  "), val.Render(m.ip), dim.Render(m.isp))
	fmt.Fprintf(&b, "  %s  %s, %s\n", label.Render("Coord   "), val.Render(m.lat), val.Render(m.lon))
	fmt.Fprintf(&b, "  %s  %s (%s, %s) · %.0f km · id %s\n",
		label.Render("Server  "), val.Render(m.srvSponsor),
		val.Render(m.srvName), val.Render(m.srvCountry), m.distance, dim.Render(m.srvID))
	fmt.Fprintln(&b, bar)
	fmt.Fprintf(&b, "  %s  %s\n", label.Render("Ping    "), pingColor(m.ping).Render(fmt.Sprintf("%.2f ms", m.ping)))
	fmt.Fprintf(&b, "  %s  %s\n", label.Render("Jitter  "), pingColor(m.jitter).Render(fmt.Sprintf("%.2f ms", m.jitter)))
	fmt.Fprintf(&b, "  %s  %s\n", label.Render("Download"), bigDown.Render(fmt.Sprintf("%.2f Mbps", m.dlMbps)))
	fmt.Fprintf(&b, "  %s  %s\n", label.Render("Upload  "), bigUp.Render(fmt.Sprintf("%.2f Mbps", m.ulMbps)))
	fmt.Fprintln(&b, bar)
	fmt.Fprintln(&b)
	return b.String()
}
