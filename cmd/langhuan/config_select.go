package main

import (
	"fmt"
	"os"
)

// ConfigSelection 描述一次启动选定的配置来源。
type ConfigSelection struct {
	Path     string
	Explicit bool // 用户是否显式传 -config
}

// resolveConfigSelection 实现 spec §2.1 的四态有序探测链：
//  1. 显式 -config <path>（explicitSet=true）：严格使用，不可访问即失败
//  2. 当前目录 config.yaml（cwdConfig）
//  3. ~/.langhuan-data/config.yaml（dataDirConfig）：standalone 兜底落盘物
//  4. 以上都不存在：调用 generator 生成 standalone config（含 credential.key）
//
// 各层文件存在但内容损坏/校验失败由后续 config.Load 报错（fail-fast），
// 探测阶段只判断存在性。generator 仅在第 4 层调用——已存在的 dataDirConfig
// 即使损坏也不会触发生成（避免静默覆盖用户编辑或失败的生产配置）。
func resolveConfigSelection(
	explicitPath string, explicitSet bool,
	cwdConfig, dataDirConfig, dataDirPath string,
	generator func(dataDirPath string) (string, error),
) (ConfigSelection, error) {
	if explicitSet {
		if _, err := os.Stat(explicitPath); err != nil {
			return ConfigSelection{}, fmt.Errorf("显式配置 %s 不可访问: %w", explicitPath, err)
		}
		return ConfigSelection{Path: explicitPath, Explicit: true}, nil
	}
	if path, ok, err := statIfExists(cwdConfig); err != nil {
		return ConfigSelection{}, err
	} else if ok {
		return ConfigSelection{Path: path}, nil
	}
	if path, ok, err := statIfExists(dataDirConfig); err != nil {
		return ConfigSelection{}, err
	} else if ok {
		return ConfigSelection{Path: path}, nil
	}
	generated, err := generator(dataDirPath)
	if err != nil {
		return ConfigSelection{}, fmt.Errorf("生成 standalone 配置失败: %w", err)
	}
	return ConfigSelection{Path: generated}, nil
}

// statIfExists 返回 (path, true, nil) 当文件存在；(false, nil) 当 IsNotExist；
// 任何其它 Stat 错误（权限/IO）原样返回（fail-fast，spec §2.1）。
func statIfExists(path string) (string, bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return path, true, nil
	}
	if os.IsNotExist(err) {
		return "", false, nil
	}
	return "", false, fmt.Errorf("检查配置 %s 失败: %w", path, err)
}
