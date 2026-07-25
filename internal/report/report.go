package report

import "time"

// Result 保存單一 job 的識別、時間、完成檔案數與最終錯誤。
type Result struct {
	Name      string
	Type      string
	StartedAt time.Time
	Duration  time.Duration
	Files     int
	Err       error
}

// Successful 判斷 Result 是否沒有執行錯誤。
// 輸入為 Result 接收者；輸出為 Err 等於 nil 的布林值。
func (r Result) Successful() bool { return r.Err == nil }
