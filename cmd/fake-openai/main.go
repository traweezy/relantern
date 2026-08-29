package main

import "github.com/traweezy/relantern/internal/fakeprovider"

func main() {
	fakeprovider.Main(fakeprovider.KindOpenAI, 8091)
}
