// 本目录是 Wails 的平台模板目录，不是本项目的一部分。
//
// 隔离理由：build/ios/app_options_default.go 使用 //go:build !ios 约束，
// 在 Windows 上会参与编译，而该目录是 package main 却没有 main 函数，
// 导致 go build ./... 失败。
//
// 独立 module 后，父模块的 ./... 不再遍历此处（Go 的 module 边界规则）。
// 这也是唯一不删文件、不影响 wails3 打包的解法。
module github.com/ADWMC/helm-x/build/android

go 1.26.7
