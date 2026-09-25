package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const maxConfigBytes = 1024 * 1024

// LoadConfig 只读加载配置，不在普通启动时进入交互或改写文件
func LoadConfig(path string) (*Config, error) {
	data, err := readConfigFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("配置文件不存在，请使用 -config 指定路径，或使用 -init 初始化配置: %w", err)
		}
		return nil, err
	}
	conf, err := decodeConfig(data)
	if err != nil {
		return nil, err
	}
	mutex.Lock()
	instance = conf
	mutex.Unlock()
	return conf, nil
}

// InitializeConfig 显式进入首次配置流程，不覆盖已有文件
func InitializeConfig(path string) error {
	if _, err := os.Lstat(path); err == nil {
		return fmt.Errorf("配置文件已经存在，不会覆盖")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	conf := DefaultConfig()
	if err := SetConfigByInput(conf); err != nil {
		return fmt.Errorf("初始化配置已中止: %w", err)
	}
	data, err := marshalConfig(conf)
	if err != nil {
		return err
	}
	return writeConfigFile(path, data, nil)
}

// UpdateConfig 显式迁移配置，保留原文件的独立备份
func UpdateConfig(path string) (string, error) {
	original, err := readConfigFile(path)
	if err != nil {
		return "", err
	}
	conf, err := decodeConfig(original)
	if err != nil {
		return "", err
	}
	data, err := marshalConfig(conf)
	if err != nil {
		return "", err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("只允许迁移普通配置文件")
	}
	backup, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".backup-*")
	if err != nil {
		return "", fmt.Errorf("创建配置备份失败: %w", err)
	}
	backupPath := backup.Name()
	_, writeErr := backup.Write(original)
	err = errors.Join(writeErr, backup.Sync(), backup.Close())
	if err != nil {
		_ = os.Remove(backupPath)
		return "", fmt.Errorf("保存配置备份失败: %w", err)
	}
	if err := writeConfigFile(path, data, original); err != nil {
		return backupPath, err
	}
	return backupPath, nil
}

// readConfigFile 限制配置大小，避免错误文件耗尽内存
func readConfigFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxConfigBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxConfigBytes {
		return nil, fmt.Errorf("配置文件超过 1 MiB 上限")
	}
	return data, nil
}

// decodeConfig 按字段出现性覆盖默认值，拒绝未知字段及多个 YAML 文档
func decodeConfig(data []byte) (*Config, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, fmt.Errorf("配置文件为空")
	}
	conf := DefaultConfig()
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(conf); err != nil {
		return nil, fmt.Errorf("解析配置失败: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("配置文件只能包含一个 YAML 文档")
	}
	return conf, nil
}

// writeConfigFile 使用同目录临时文件；初始化不覆盖，迁移不覆盖并发修改
func writeConfigFile(path string, data, original []byte) error {
	if _, err := decodeConfig(data); err != nil {
		return fmt.Errorf("待保存配置无效: %w", err)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, ".glyccat-config-*")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	_, writeErr := file.Write(data)
	if err := errors.Join(writeErr, file.Sync(), file.Close()); err != nil {
		return fmt.Errorf("写入临时配置失败: %w", err)
	}
	if original == nil {
		// 硬链接以“不存在才创建”的方式提交完整文件，防止并发初始化覆盖。
		if err := os.Link(name, path); err != nil {
			return fmt.Errorf("创建配置文件失败，已有文件不会被覆盖: %w", err)
		}
		return nil
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("原配置文件已被移除或替换，取消迁移")
	}
	current, err := readConfigFile(path)
	if err != nil {
		return err
	}
	if !bytes.Equal(current, original) {
		return fmt.Errorf("原配置文件已被其他程序修改，取消迁移")
	}
	if err := os.Rename(name, path); err != nil {
		return fmt.Errorf("替换配置文件失败，原始备份已保留: %w", err)
	}
	return nil
}
