package utils

import "strings"

func Protocols2String(p []string) string {
	for i := range p {
		p[i] = p[i] + "协议"
	}
	return strings.Join(p, "，")
}
