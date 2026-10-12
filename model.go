//go:build windows

package main

import (
	"fmt"
	"time"

	"github.com/lxn/walk"

	"pingtool/internal/pinger"
)

// Table columns. Keep the order in sync with the Columns definition in app.go.
const (
	colTarget = iota
	colKind
	colSent
	colRecv
	colFail
	colLoss
	colLast
	colMin
	colAvg
	colMax
	colState
	colCount
)

type targetModel struct {
	walk.TableModelBase
	rows []pinger.TargetStatus
}

func newTargetModel() *targetModel {
	return &targetModel{}
}

func (m *targetModel) RowCount() int {
	return len(m.rows)
}

func (m *targetModel) Value(row, col int) interface{} {
	if row < 0 || row >= len(m.rows) {
		return ""
	}
	s := m.rows[row]

	switch col {
	case colTarget:
		return s.Label()
	case colKind:
		return string(s.Kind)
	case colSent:
		return s.Sent
	case colRecv:
		return s.Recv
	case colFail:
		return s.Fail
	case colLoss:
		return formatPercent(s.Loss())
	case colLast:
		if s.Sent == 0 {
			return "--"
		}
		if s.LastOK {
			return msText(s.LastRTT)
		}
		return "不通"
	case colMin:
		return optMs(s.MinRTT, s.Recv)
	case colAvg:
		return optMs(s.AvgRTT, s.Recv)
	case colMax:
		return optMs(s.MaxRTT, s.Recv)
	case colState:
		switch {
		case s.Done:
			if s.Recv == 0 && s.Sent > 0 {
				return "全部失败"
			}
			return "已完成"
		case s.Running:
			return "测试中"
		default:
			return "等待"
		}
	}
	return ""
}

// setAll replaces the model content and notifies the view.
func (m *targetModel) setAll(rows []pinger.TargetStatus) {
	reset := len(m.rows) != len(rows)
	m.rows = rows
	if reset {
		m.PublishRowsReset()
		return
	}
	if len(rows) > 0 {
		m.PublishRowsChanged(0, len(rows)-1)
	}
}

func optMs(d time.Duration, have int) string {
	if have == 0 {
		return "--"
	}
	return msText(d)
}

func formatPercent(v float64) string {
	if v <= 0 {
		return "0%"
	}
	return fmt.Sprintf("%.1f%%", v)
}
