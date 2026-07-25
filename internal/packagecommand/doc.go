// Package packagecommand 將受允許的 package manager/action 映射為固定命令。
//
// 本模組輸入 manager、action 與 cache/output/workspace 路徑，輸出不可由任務 YAML
// 任意改寫的 executable、參數及必要環境變數。
package packagecommand
