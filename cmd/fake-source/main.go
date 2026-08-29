package main

import "github.com/traweezy/relantern/internal/fakeprovider"

func main() {
	fakeprovider.Main(fakeprovider.KindSource, 8090)
}
