package main

import (
	"log"
	"os"
	"runtime"
)

var port = ""

func init() {
	if x := os.Getenv("DEV"); x != "" {
		dev = x
	}
}

func main() {
	log.SetFlags(log.Ltime | log.Lmicroseconds)
	port = os.Getenv("SERVER_PORT")
	if port == "8000" || port == "8001" {
		runtime.GOMAXPROCS(8)
		runFilter()
	}
	if port == "8002" {
		runtime.GOMAXPROCS(15)
		runBackEnd()
	}
	if port == "9000" || port == "10009" {
		runScoring()
	}
}
