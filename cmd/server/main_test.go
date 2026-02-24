// Copyright 2023 LiveKit, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"os"
	"testing"    // Go 语言的标准测试包

	// 第三方断言库，提供更便捷的测试断言功能
	"github.com/stretchr/testify/require"
)

// 定义测试用的结构体，用于组织多个测试用例
type testStruct struct {
	// 配置文件名，可能为空
	configFileName string
	// 配置文件内容字符串，可能为空
	configBody     string

	// 期望的错误结果，nil 表示期望无错误
	expectedError      error
	// 期望的最终配置内容
	expectedConfigBody string
}

// 定义测试函数 TestGetConfigString，用于测试 getConfigString 函数的各种情况
func TestGetConfigString(t *testing.T) {
	// 定义测试用例切片，包含多个测试场景
	tests := []testStruct{
		// 测试场景1：没有配置文件和配置内容，期望返回空字符串
		{"", "", nil, ""},
		// 测试场景2：没有配置文件但有配置内容，期望返回配置内容
		{"", "configBody", nil, "configBody"},
		// 测试场景3：有配置文件名和配置内容，期望优先返回配置内容
		{"file", "configBody", nil, "configBody"},
		// 测试场景4：有配置文件名但没有配置内容，期望从文件读取内容
		{"file", "", nil, "fileContent"},
	}

	// 遍历所有测试用例
	for _, test := range tests {
		// 使用匿名函数包裹每个测试用例，实现隔离
		func() {
			// 调用辅助函数创建配置文件
			writeConfigFile(test, t)
			// 延迟删除配置文件，确保测试后清理
			defer os.Remove(test.configFileName)

			// 调用被测试的函数 getConfigString
			configBody, err := getConfigString(test.configFileName, test.configBody)
			// 使用 require 断言验证错误是否符合预期
			require.Equal(t, test.expectedError, err)
			// 使用 require 断言验证返回的配置内容是否符合预期
			require.Equal(t, test.expectedConfigBody, configBody)
		}() // 立即执行这个匿名函数
	}
}

// 测试当配置文件不存在时是否返回错误
func TestShouldReturnErrorIfConfigFileDoesNotExist(t *testing.T) {
	// 调用 getConfigString，传入不存在的文件名
	configBody, err := getConfigString("notExistingFile", "")
	// 验证确实返回了错误
	require.Error(t, err)
	// 验证返回的配置内容为空
	require.Empty(t, configBody)
}

// 辅助函数 writeConfigFile，用于创建测试用的配置文件
func writeConfigFile(test testStruct, t *testing.T) {
	// 检查测试用例中是否指定了配置文件名
	if test.configFileName != "" {
		// 将期望的配置内容转换为字节切片
		d1 := []byte(test.expectedConfigBody)
		// 写入配置文件，权限设置为 0o644（八进制，表示文件所有者可读写，组用户和其他用户只读）
		err := os.WriteFile(test.configFileName, d1, 0o644)
		// 断言文件写入操作没有错误
		require.NoError(t, err)
	}
}
