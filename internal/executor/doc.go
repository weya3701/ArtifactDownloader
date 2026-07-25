// Package executor 封裝不經 shell 的外部程式執行。
//
// 本模組輸入 context、執行檔、參數及工作目錄與環境選項，將輸出導向指定 writer，
// 並輸出包含程式啟動或結束失敗原因的錯誤。
package executor
