// Code generated by hertz generator.

package main

import (
	"github.com/cloudwego/hertz/pkg/app/server"
)

func main() {
	h := server.Default()

	h.LoadHTMLGlob("html/**/*")

	register(h)
	h.Spin()
}
