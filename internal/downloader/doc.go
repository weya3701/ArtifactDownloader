// Package downloader 提供 HTTP/HTTPS artifact 下載與安全檔名解析功能。
//
// 本模組輸入來源 URL、目的路徑與覆寫策略，透過暫存檔與 rename 發布完整檔案，
// 輸出下載錯誤或由 URL 解析出的安全檔名。
package downloader
