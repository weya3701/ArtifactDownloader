// Package repository 封裝 Git repository 的 clone 與 ref checkout。
//
// 本模組輸入 repository 設定、目的目錄與 context，安全組合 Git 參數並執行；
// 成功時輸出已 checkout 的工作目錄，失敗時回傳帶階段資訊的錯誤。
package repository
