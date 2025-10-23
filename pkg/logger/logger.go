package logger

import "log"

const MaxLogLevel = 3

func SetLogFormat() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
}

// LogWithLevel 根据日志级别打印日志
// rl: real level 用户传入日志级别
// ol: on level 指定的日志级别
// v: 日志内容
func LogWithLevel(rl int, ol int, v interface{}) {
	if rl <= 1 && ol == 1 {
		log.Println(v)
		return
	}

	if rl > MaxLogLevel {
		log.Println(v)
		return
	}

	if rl >= ol {
		log.Println(v)
	}
}
