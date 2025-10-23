package logger

import (
	"fmt"
	"log"
)

const MaxLogLevel = 3

func SetLogFormat() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
}

// LogWithLevel 根据日志级别打印日志
// rl: real level 用户传入日志级别
// ol: on level 指定的日志级别
// v: 日志内容
func LogWithLevel(rl int, ol int, v interface{}) {
	// 判断是否需要打印
	if (rl <= 1 && ol == 1) || rl > MaxLogLevel || rl >= ol {
		// call depth = 2 表示向上两层获取文件名和行号
		log.Output(2, fmt.Sprint(v))
	}
}
