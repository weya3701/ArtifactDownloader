// Package environmentconfig 管理與任務設定分離的受信任執行環境政策。
//
// 本模組輸入環境政策 YAML 與 package manager，驗證可繼承、固定及映射的變數，
// 輸出傳給 package 子程序的最小環境集合。
package environmentconfig
