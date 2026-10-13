//go:build windows

package main

import (
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"

	"pingtool/internal/pinger"
)

const appTitle = "网络连通性批量测试工具  —  ICMP / TCPing"

var defaultTargets = strings.Join([]string{
	"# 每行一个目标：IP / 域名 / host:端口 / 网段(如 192.168.1.0/24) / 地址范围(如 192.168.1.1-254)",
	"8.8.8.8",
	"1.1.1.1",
	"223.5.5.5",
	"www.baidu.com:443",
}, "\n")

// App holds all UI state.
type App struct {
	mw *walk.MainWindow

	targetsEdit  *walk.TextEdit
	runModeCombo *walk.ComboBox
	countEdit    *walk.NumberEdit
	durEdit      *walk.NumberEdit
	durUnitCombo *walk.ComboBox
	intervalEdit *walk.NumberEdit
	timeoutEdit  *walk.NumberEdit
	concEdit     *walk.NumberEdit
	threshEdit   *walk.NumberEdit
	startBtn     *walk.PushButton
	stopBtn      *walk.PushButton
	summary      *walk.Label

	tv    *walk.TableView
	model *targetModel

	grid         *walk.CustomWidget
	chart        *walk.CustomWidget
	gridR        *gridRenderer
	detailTitle  *walk.Label
	detailLatest *walk.Label
	detailStats  *walk.Label
	detailInfo   *walk.Label

	engine     *pinger.Engine
	selected   int
	threshold  time.Duration
	gridProbes []pinger.Probe
	gridTotal  int
	dirty      bool
	closed     atomic.Bool

	lastSummary string
	lastTitle   string
	lastLatest  string
	lastStats   string
	lastInfo    string
}

func main() {
	a := &App{
		selected: -1,
		gridR:    newGridRenderer(),
		model:    newTargetModel(),
	}
	if err := a.build(); err != nil {
		walk.MsgBox(nil, "启动失败", err.Error(), walk.MsgBoxIconError)
		return
	}
	a.startRefreshLoop()
	a.mw.Run()
	a.closed.Store(true)
	if a.engine != nil {
		a.engine.Stop()
	}
}

func (a *App) build() error {
	err := MainWindow{
		AssignTo: &a.mw,
		Title:    appTitle,
		MinSize:  Size{Width: 1060, Height: 700},
		Size:     Size{Width: 1400, Height: 830},
		Layout:   VBox{},
		Children: []Widget{
			Composite{
				Layout: HBox{},
				Children: []Widget{
					GroupBox{
						Title:         "① 目标地址（每行一个，支持批量）",
						Layout:        VBox{},
						StretchFactor: 3,
						MinSize:       Size{Width: 360, Height: 0},
						Children: []Widget{
							TextEdit{
								AssignTo: &a.targetsEdit,
								VScroll:  true,
								HScroll:  true,
								Text:     defaultTargets,
								MinSize:  Size{Width: 340, Height: 110},
							},
							Label{Text: "格式：192.168.1.1 → ICMP；192.168.1.1:22 → TCPing；192.168.1.0/24 或 192.168.1.1-254 → 网段扫描（可加 :端口）  /  example.com / [::1]:443 / tcp://host:port（# 后为注释）"},
							Composite{
								Layout: HBox{},
								Children: []Widget{
									PushButton{Text: "从文件载入…", OnClicked: a.onLoadFile},
									PushButton{Text: "本机网段", OnClicked: a.onAddLocalSubnets},
									PushButton{Text: "清空", OnClicked: a.onClearTargets},
									PushButton{Text: "导出结果 CSV…", OnClicked: a.onExportCSV},
									PushButton{Text: "导出详细日志…", OnClicked: a.onExportLog},
								},
							},
						},
					},
					GroupBox{
						Title:         "② 测试参数",
						Layout:        Grid{Columns: 4},
						StretchFactor: 4,
						Children: []Widget{
							Label{Text: "测试方式"},
							ComboBox{AssignTo: &a.runModeCombo, Model: []string{"按包数", "按时长", "持续测试"}, CurrentIndex: 0, OnCurrentIndexChanged: a.onRunModeChanged, MinSize: Size{Width: 190, Height: 0}},
							Label{Text: "包数"},
							NumberEdit{AssignTo: &a.countEdit, MinValue: 1, MaxValue: 1000000, Value: 50.0, Decimals: 0, SpinButtonsVisible: true},

							Label{Text: "时长"},
							Composite{
								Layout: HBox{MarginsZero: true},
								Children: []Widget{
									NumberEdit{AssignTo: &a.durEdit, MinValue: 1, MaxValue: 1000000, Value: 60.0, Decimals: 0, SpinButtonsVisible: true, Enabled: false, MinSize: Size{Width: 90, Height: 0}},
									ComboBox{AssignTo: &a.durUnitCombo, Model: []string{"秒", "分", "时"}, CurrentIndex: 0, Enabled: false, MinSize: Size{Width: 68, Height: 0}},
								},
							},
							Label{Text: "间隔（毫秒）"},
							NumberEdit{AssignTo: &a.intervalEdit, MinValue: 50, MaxValue: 600000, Value: 1000.0, Decimals: 0, SpinButtonsVisible: true},

							Label{Text: "超时（毫秒）"},
							NumberEdit{AssignTo: &a.timeoutEdit, MinValue: 50, MaxValue: 60000, Value: 2000.0, Decimals: 0, SpinButtonsVisible: true},
							Label{Text: "并发数"},
							NumberEdit{AssignTo: &a.concEdit, MinValue: 1, MaxValue: 5000, Value: 100.0, Decimals: 0, SpinButtonsVisible: true},

							Label{Text: "高延迟告警（毫秒，0=关闭）"},
							NumberEdit{AssignTo: &a.threshEdit, MinValue: 0, MaxValue: 600000, Value: 100.0, Decimals: 0, SpinButtonsVisible: true},
							PushButton{AssignTo: &a.startBtn, Text: "▶ 开始测试", OnClicked: a.onStart},
							PushButton{AssignTo: &a.stopBtn, Text: "■ 停止", Enabled: false, OnClicked: a.onStop},
						},
					},
				},
			},
			Composite{
				Layout:        HBox{},
				StretchFactor: 1,
				Children: []Widget{
					TableView{
						AssignTo:         &a.tv,
						Model:            a.model,
						AlternatingRowBG: true,
						StretchFactor:    3,
						MinSize:          Size{Width: 520, Height: 240},
						Columns: []TableViewColumn{
							{Title: "目标", Width: 210},
							{Title: "协议", Width: 52},
							{Title: "已发", Width: 52, Alignment: AlignFar},
							{Title: "成功", Width: 52, Alignment: AlignFar},
							{Title: "失败", Width: 52, Alignment: AlignFar},
							{Title: "丢包率", Width: 66, Alignment: AlignFar},
							{Title: "最新(ms)", Width: 72, Alignment: AlignFar},
							{Title: "最小(ms)", Width: 72, Alignment: AlignFar},
							{Title: "平均(ms)", Width: 72, Alignment: AlignFar},
							{Title: "最大(ms)", Width: 72, Alignment: AlignFar},
							{Title: "状态", Width: 80},
						},
						OnCurrentIndexChanged: a.onSelect,
						StyleCell:             a.styleCell,
					},
					GroupBox{
						Title:         "③ 选中目标详情",
						Layout:        VBox{},
						StretchFactor: 1,
						MaxSize:       Size{Width: 430, Height: 0},
						MinSize:       Size{Width: 360, Height: 0},
						Children: []Widget{
							Label{AssignTo: &a.detailTitle, Text: "（未选择目标）", Font: Font{PointSize: 11, Bold: true}},
							Label{AssignTo: &a.detailLatest, Text: "-- ms", Font: Font{PointSize: 26, Bold: true}},
							Label{Text: "状态格（1 格 = 1 个探测包／1 个时间片）"},
							CustomWidget{
								AssignTo:            &a.grid,
								PaintPixels:         a.paintGrid,
								MinSize:             Size{Width: 340, Height: 96},
								InvalidatesOnResize: true,
							},
							Label{Text: "延迟曲线（折线图）"},
							CustomWidget{
								AssignTo:            &a.chart,
								PaintPixels:         a.paintChart,
								MinSize:             Size{Width: 340, Height: 110},
								InvalidatesOnResize: true,
							},
							Label{AssignTo: &a.detailStats, Text: "—"},
							Label{AssignTo: &a.detailInfo, Text: "—"},
							Composite{
								Layout: HBox{},
								Children: []Widget{
									Label{Text: "图例：", TextColor: textPlain},
									Label{Text: "■ 正常", TextColor: textOK},
									Label{Text: "■ 高延迟", TextColor: textSlow},
									Label{Text: "■ 不通", TextColor: textFail},
								},
							},
						},
					},
				},
			},
			Composite{
				Layout: HBox{},
				Children: []Widget{
					Label{AssignTo: &a.summary, Text: "就绪。请设置参数后点击“开始测试”。"},
				},
			},
		},
	}.Create()
	if err != nil {
		return err
	}

	// Restore the last used inputs (缓存); on the very first run this keeps the
	// declarative defaults and selects 持续测试.
	a.applySettings()

	a.mw.Closing().Attach(func(canceled *bool, reason walk.CloseReason) {
		a.saveSettings()
		a.closed.Store(true)
		if a.engine != nil {
			a.engine.Stop()
		}
	})
	return nil
}

// ---------------------------------------------------------------- settings

const (
	// runModeEntries is the number of items in 测试方式 (按包数 / 按时长 / 持续测试).
	runModeEntries = 3
	// continuousIndex is the 持续测试 item, the default on a first run.
	continuousIndex = 2
)

func clampIndex(v, n int) int {
	if v < 0 {
		return 0
	}
	if v >= n {
		return n - 1
	}
	return v
}

// applySettings restores the last used address list and parameters (缓存).
// When there is no settings file yet the declarative defaults are kept and the
// run mode stays on 持续测试.
func (a *App) applySettings() {
	s, err := readSettings()
	if err != nil || s == nil {
		a.runModeCombo.SetCurrentIndex(continuousIndex)
		a.onRunModeChanged()
		return
	}
	if s.Targets != "" {
		a.targetsEdit.SetText(s.Targets)
	}
	a.runModeCombo.SetCurrentIndex(clampIndex(s.RunMode, runModeEntries))
	if s.Count > 0 {
		a.countEdit.SetValue(float64(s.Count))
	}
	if s.Duration > 0 {
		a.durEdit.SetValue(float64(s.Duration))
	}
	a.durUnitCombo.SetCurrentIndex(clampIndex(s.DurUnit, 3))
	if s.IntervalMS > 0 {
		a.intervalEdit.SetValue(float64(s.IntervalMS))
	}
	if s.TimeoutMS > 0 {
		a.timeoutEdit.SetValue(float64(s.TimeoutMS))
	}
	if s.Concurrency > 0 {
		a.concEdit.SetValue(float64(s.Concurrency))
	}
	if s.ThresholdMS >= 0 {
		a.threshEdit.SetValue(float64(s.ThresholdMS))
	}
	a.onRunModeChanged()
}

// saveSettings persists the current inputs so the next launch restores them.
// It is best effort: a failure to write the cache must never block the app.
func (a *App) saveSettings() {
	_ = writeSettings(&Settings{
		Targets:     a.targetsEdit.Text(),
		RunMode:     a.runModeCombo.CurrentIndex(),
		Count:       int(a.countEdit.Value()),
		Duration:    int(a.durEdit.Value()),
		DurUnit:     a.durUnitCombo.CurrentIndex(),
		IntervalMS:  int(a.intervalEdit.Value()),
		TimeoutMS:   int(a.timeoutEdit.Value()),
		Concurrency: int(a.concEdit.Value()),
		ThresholdMS: int(a.threshEdit.Value()),
	})
}

// ---------------------------------------------------------------- handlers

func (a *App) onRunModeChanged() {
	switch a.runModeCombo.CurrentIndex() {
	case 1: // 按时长
		a.countEdit.SetEnabled(false)
		a.durEdit.SetEnabled(true)
		a.durUnitCombo.SetEnabled(true)
	case 2: // 持续测试
		a.countEdit.SetEnabled(false)
		a.durEdit.SetEnabled(false)
		a.durUnitCombo.SetEnabled(false)
	default: // 按包数
		a.countEdit.SetEnabled(true)
		a.durEdit.SetEnabled(false)
		a.durUnitCombo.SetEnabled(false)
	}
}

// durUnit returns the multiplier selected next to the duration field.
func (a *App) durUnit() time.Duration {
	switch a.durUnitCombo.CurrentIndex() {
	case 1:
		return time.Minute
	case 2:
		return time.Hour
	default:
		return time.Second
	}
}

func (a *App) onClearTargets() {
	a.targetsEdit.SetText("")
}

// onAddLocalSubnets appends the subnets of the local IPv4 interfaces so the
// user can sweep the LAN without typing the CIDR by hand.
func (a *App) onAddLocalSubnets() {
	subs := pinger.LocalSubnets()
	if len(subs) == 0 {
		walk.MsgBox(a.mw, "未找到网段", "没有检测到可用的 IPv4 网络接口。", walk.MsgBoxIconWarning)
		return
	}
	cur := strings.TrimRight(a.targetsEdit.Text(), "\r\n")
	add := strings.Join(subs, "\r\n")
	if cur != "" {
		cur += "\r\n"
	}
	a.targetsEdit.SetText(cur + add)
}

func (a *App) onLoadFile() {
	dlg := new(walk.FileDialog)
	dlg.Title = "选择目标列表文件（每行一个地址）"
	dlg.Filter = "文本文件 (*.txt;*.csv)|*.txt;*.csv|所有文件 (*.*)|*.*"
	ok, err := dlg.ShowOpen(a.mw)
	if err != nil {
		walk.MsgBox(a.mw, "错误", err.Error(), walk.MsgBoxIconError)
		return
	}
	if !ok {
		return
	}
	b, err := os.ReadFile(dlg.FilePath)
	if err != nil {
		walk.MsgBox(a.mw, "错误", "读取文件失败："+err.Error(), walk.MsgBoxIconError)
		return
	}
	a.targetsEdit.SetText(string(b))
}

func (a *App) onExportCSV() {
	if a.engine == nil {
		walk.MsgBox(a.mw, "提示", "还没有可导出的测试结果。", walk.MsgBoxIconInformation)
		return
	}
	dlg := new(walk.FileDialog)
	dlg.Title = "导出测试结果"
	dlg.Filter = "CSV 文件 (*.csv)|*.csv|所有文件 (*.*)|*.*"
	dlg.FilePath = "ping-result.csv"
	ok, err := dlg.ShowSave(a.mw)
	if err != nil {
		walk.MsgBox(a.mw, "错误", err.Error(), walk.MsgBoxIconError)
		return
	}
	if !ok {
		return
	}
	// The walk save dialog does not append the filter extension, so do it here.
	dlg.FilePath = ensureExt(dlg.FilePath, ".csv")
	snap := a.engine.Snapshot()
	var sb strings.Builder
	sb.WriteString("目标,协议,已发,成功,失败,丢包率(%),最新(ms),最小(ms),平均(ms),最大(ms),状态\n")
	for _, s := range snap {
		last := "不通"
		if s.LastOK {
			last = msText(s.LastRTT)
		}
		fmt.Fprintf(&sb, "%s,%s,%d,%d,%d,%.2f,%s,%s,%s,%s,%s\n",
			s.Label(), s.Kind, s.Sent, s.Recv, s.Fail, s.Loss(), last,
			optMs(s.MinRTT, s.Recv), optMs(s.AvgRTT, s.Recv), optMs(s.MaxRTT, s.Recv),
			stateText(s))
	}
	if err := os.WriteFile(dlg.FilePath, []byte(sb.String()), 0644); err != nil {
		walk.MsgBox(a.mw, "错误", "写入失败："+err.Error(), walk.MsgBoxIconError)
		return
	}
	walk.MsgBox(a.mw, "完成", "已导出到：\n"+dlg.FilePath, walk.MsgBoxIconInformation)
}

// onExportLog writes a plain text log that keeps every individual probe in the
// style of the ping / tcping command output, so the export holds the full
// continuous result instead of only the per-target summary.
func (a *App) onExportLog() {
	if a.engine == nil {
		walk.MsgBox(a.mw, "提示", "还没有可导出的测试结果。", walk.MsgBoxIconInformation)
		return
	}
	dlg := new(walk.FileDialog)
	dlg.Title = "导出详细日志（连续输出）"
	dlg.Filter = "文本日志 (*.txt;*.log)|*.txt;*.log|所有文件 (*.*)|*.*"
	dlg.FilePath = "ping-log.txt"
	ok, err := dlg.ShowSave(a.mw)
	if err != nil {
		walk.MsgBox(a.mw, "错误", err.Error(), walk.MsgBoxIconError)
		return
	}
	if !ok {
		return
	}
	// The walk save dialog does not append the filter extension, so do it here.
	dlg.FilePath = ensureExt(dlg.FilePath, ".txt")

	snap := a.engine.Snapshot()
	cfg := a.engine.Config()
	var sb strings.Builder
	sb.WriteString("# 网络连通性测试详细日志\n")
	fmt.Fprintf(&sb, "# 导出时间：%s\n", time.Now().Format("2006-01-02 15:04:05"))
	fmt.Fprintf(&sb, "# 测试方式：%s ｜ 间隔 %d ms ｜ 超时 %d ms ｜ 并发 %d\n\n",
		modeText(cfg), cfg.Interval.Milliseconds(), cfg.Timeout.Milliseconds(), cfg.Concurrency)

	prompt := localPrompt()
	for i, s := range snap {
		writeProbeLog(&sb, s, a.engine.Probes(i), cfg, prompt)
	}

	if err := os.WriteFile(dlg.FilePath, []byte(sb.String()), 0644); err != nil {
		walk.MsgBox(a.mw, "错误", "写入失败："+err.Error(), walk.MsgBoxIconError)
		return
	}
	walk.MsgBox(a.mw, "完成", "已导出到：\n"+dlg.FilePath, walk.MsgBoxIconInformation)
}

func (a *App) onSelect() {
	a.selected = a.tv.CurrentIndex()
	if a.engine != nil {
		snap := a.engine.Snapshot()
		if a.selected >= 0 && a.selected < len(snap) {
			a.gridProbes = a.engine.Probes(a.selected)
			a.updateDetail(snap[a.selected])
			a.grid.Invalidate()
			a.chart.Invalidate()
		}
	}
}

func (a *App) onStop() {
	if a.engine != nil {
		a.engine.Stop()
	}
	a.stopBtn.SetEnabled(false)
	a.startBtn.SetEnabled(true)
}

func (a *App) onStart() {
	if a.engine != nil && a.engine.Running() {
		return
	}

	// Protocol is derived per target: 192.168.1.1 → ICMP, 192.168.1.1:22 → TCP.
	opt := pinger.ParseOptions{Protocol: pinger.ProtocolAuto}

	targets, errs := parseTargets(a.targetsEdit.Text(), opt)
	if len(targets) == 0 {
		msg := "没有可测试的目标地址。"
		if len(errs) > 0 {
			msg += "\n\n" + strings.Join(errs, "\n")
		}
		walk.MsgBox(a.mw, "没有目标", msg, walk.MsgBoxIconWarning)
		return
	}

	cfg := pinger.Config{
		Interval:    time.Duration(a.intervalEdit.Value()) * time.Millisecond,
		Timeout:     time.Duration(a.timeoutEdit.Value()) * time.Millisecond,
		Concurrency: int(a.concEdit.Value()),
	}
	switch a.runModeCombo.CurrentIndex() {
	case 0: // 按包数
		cfg.Mode = pinger.ModeCount
		cfg.Count = int(a.countEdit.Value())
	case 1: // 按时长
		cfg.Mode = pinger.ModeDuration
		cfg.Duration = time.Duration(a.durEdit.Value() * float64(a.durUnit()))
	default: // 持续测试：直到手动停止
		cfg.Mode = pinger.ModeContinuous
	}

	a.threshold = time.Duration(a.threshEdit.Value()) * time.Millisecond

	a.engine = pinger.New(cfg, targets)
	a.gridTotal = expectedCells(cfg)
	a.gridProbes = nil
	a.selected = 0
	a.dirty = true

	a.model.setAll(a.engine.Snapshot())
	a.tv.SetCurrentIndex(0)
	a.engine.Start()

	a.startBtn.SetEnabled(false)
	a.stopBtn.SetEnabled(true)
	a.saveSettings()

	if len(errs) > 0 {
		a.summary.SetText(fmt.Sprintf("已忽略 %d 行无效目标：%s", len(errs), strings.Join(errs, "；")))
	}
}

func (a *App) styleCell(style *walk.CellStyle) {
	row, col := style.Row(), style.Col()
	if row < 0 || row >= a.model.RowCount() {
		return
	}
	s := a.model.rows[row]

	switch col {
	case colLast:
		switch {
		case s.Sent > 0 && !s.LastOK:
			style.TextColor = textFail
		case s.LastOK && a.threshold > 0 && s.LastRTT > a.threshold:
			style.TextColor = textSlow
		case s.LastOK:
			style.TextColor = textOK
		}
	case colLoss:
		if s.Sent > 0 && s.Recv < s.Sent {
			style.TextColor = textFail
		} else if s.Sent > 0 {
			style.TextColor = textOK
		}
	case colState:
		switch {
		case s.Done && s.Recv == 0 && s.Sent > 0:
			style.TextColor = textFail
		case s.Done:
			style.TextColor = textOK
		}
	}
}

// ---------------------------------------------------------------- refresh

func (a *App) startRefreshLoop() {
	go func() {
		ticker := time.NewTicker(150 * time.Millisecond)
		defer ticker.Stop()
		for range ticker.C {
			if a.closed.Load() {
				return
			}
			a.mw.Synchronize(a.refresh)
		}
	}()
}

func (a *App) refresh() {
	if a.engine == nil {
		return
	}
	running := a.engine.Running()
	if !running && !a.dirty {
		return
	}
	if !running {
		a.dirty = false
	}

	snap := a.engine.Snapshot()
	a.model.setAll(snap)

	idx := a.selected
	if idx >= len(snap) {
		idx = len(snap) - 1
	}
	if idx >= 0 && idx < len(snap) {
		a.gridProbes = a.engine.Probes(idx)
		a.updateDetail(snap[idx])
		a.grid.Invalidate()
		a.chart.Invalidate()
	}
	a.updateSummary(snap)
}

func (a *App) updateSummary(snap []pinger.TargetStatus) {
	var sent, recv, up, down int
	for _, s := range snap {
		sent += s.Sent
		recv += s.Recv
		if s.Sent > 0 {
			if s.Recv > 0 {
				up++
			} else {
				down++
			}
		}
	}
	loss := 0.0
	if sent > 0 {
		loss = float64(sent-recv) / float64(sent) * 100
	}
	state := "已完成"
	if a.engine != nil && a.engine.Running() {
		state = "测试中"
	}
	txt := fmt.Sprintf("%s ｜ 目标 %d ｜ 连通 %d ｜ 不通 %d ｜ 发送 %d ｜ 收到 %d ｜ 总丢包率 %.2f%% ｜ 高延迟阈值 %s",
		state, len(snap), up, down, sent, recv, loss, msText(a.threshold)+" ms")
	a.setLabel(a.summary, &a.lastSummary, txt)
}

func (a *App) updateDetail(s pinger.TargetStatus) {
	title := fmt.Sprintf("%s   [%s]", s.Label(), s.Kind)
	if s.IP != nil {
		title += "  →  " + s.IP.String()
	}
	a.setLabel(a.detailTitle, &a.lastTitle, title)

	latest := "-- ms"
	color := textPlain
	switch {
	case s.Sent == 0:
		latest = "等待中…"
	case s.LastOK:
		latest = msText(s.LastRTT) + " ms"
		switch {
		case a.threshold > 0 && s.LastRTT > a.threshold:
			color = textSlow
		default:
			color = textOK
		}
	default:
		latest = "不通"
		color = textFail
	}
	if latest != a.lastLatest {
		a.lastLatest = latest
		a.detailLatest.SetText(latest)
		a.detailLatest.SetTextColor(color)
	}

	stats := fmt.Sprintf("已发 %d   成功 %d   失败 %d   丢包率 %s   已运行 %s",
		s.Sent, s.Recv, s.Fail, formatPercent(s.Loss()), s.Elapsed.Truncate(time.Second))
	a.setLabel(a.detailStats, &a.lastStats, stats)

	info := fmt.Sprintf("最小 %s   平均 %s   最大 %s   抖动 %s",
		optMs(s.MinRTT, s.Recv), optMs(s.AvgRTT, s.Recv), optMs(s.MaxRTT, s.Recv),
		optMs(jitter(s), s.Recv))
	if s.Sent > 0 && s.LastErr != "" {
		info += "   ｜ 最近错误：" + s.LastErr
	}
	a.setLabel(a.detailInfo, &a.lastInfo, info)
}

func (a *App) setLabel(lbl *walk.Label, last *string, txt string) {
	if lbl == nil || txt == *last {
		return
	}
	*last = txt
	lbl.SetText(txt)
}

// ---------------------------------------------------------------- helpers

func parseTargets(text string, opt pinger.ParseOptions) ([]pinger.Target, []string) {
	var (
		targets []pinger.Target
		errs    []string
		seen    = map[string]bool{}
	)
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		t, err := pinger.ExpandTarget(line, opt)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s：%v", line, err))
			continue
		}
		for _, tg := range t {
			if seen[tg.String()] {
				continue
			}
			seen[tg.String()] = true
			targets = append(targets, tg)
		}
	}
	return targets, errs
}

func expectedCells(cfg pinger.Config) int {
	switch cfg.Mode {
	case pinger.ModeCount:
		if cfg.Count < 1 {
			return 1
		}
		return cfg.Count
	case pinger.ModeDuration:
		if cfg.Interval <= 0 {
			return 1
		}
		n := int(cfg.Duration / cfg.Interval)
		if cfg.Duration%cfg.Interval != 0 {
			n++
		}
		if n < 1 {
			n = 1
		}
		return n
	default: // 持续测试：总格数未知，由实际探测数量决定
		return 0
	}
}

func jitter(s pinger.TargetStatus) time.Duration {
	if s.MaxRTT <= s.MinRTT {
		return 0
	}
	return s.MaxRTT - s.MinRTT
}

func stateText(s pinger.TargetStatus) string {
	switch {
	case s.Done && s.Recv == 0 && s.Sent > 0:
		return "全部失败"
	case s.Done:
		return "已完成"
	case s.Running:
		return "测试中"
	default:
		return "等待"
	}
}
