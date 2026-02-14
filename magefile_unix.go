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

//go:build mage && !windows
// +build mage,!windows

package main

import (
	"syscall"
)
// lecjy 将当前进程可以打开的最大文件描述符数量设置为10000
func setULimit() error {
	// raise ulimit on unix
	// lecjy 声明一个Rlimit结构体变量，用于存储资源限制信息
	var rLimit syscall.Rlimit
	// lecjy 获取当前进程的文件描述符限制，RLIMIT_NOFILE表示获取文件描述符数量的限制
	err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &rLimit)
	if err != nil {
		return err
	}
	// lecjy Max：硬限制，最大限制
	rLimit.Max = 10000
	 // lecjy Cur：软限制，当前限制
	rLimit.Cur = 10000

	// lecjy 应用新的限制值
	return syscall.Setrlimit(syscall.RLIMIT_NOFILE, &rLimit)
}
