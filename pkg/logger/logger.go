package logger

import "log"

func SetLogFormat() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
}
