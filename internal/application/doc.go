// Package application 負責編排 Artifact Downloader 的 job 生命週期。
//
// 本模組接收已驗證的任務設定與執行 context，依序執行 URL 下載或套件下載、
// 管理逾時與 callback，最後輸出每個 job 的執行結果。
package application
