package config

import _ "embed"

// ConfigTemplate 默认配置模板
//
//go:embed template.yml
var ConfigTemplate string
