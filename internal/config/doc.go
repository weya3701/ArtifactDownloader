// Package config 定義任務 YAML 的資料模型、預設值、驗證規則與路徑解析。
//
// 本模組輸入任務設定檔路徑，輸出可供 application 模組安全使用的 Config；
// 未知欄位、缺少必要欄位或不支援的 manager/action 會以錯誤回報。
package config
