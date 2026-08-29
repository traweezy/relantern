package main

import "github.com/traweezy/relantern/internal/fakeprovider"

func main() {
	fakeprovider.Main(fakeprovider.KindDelivery, 8092)
}
