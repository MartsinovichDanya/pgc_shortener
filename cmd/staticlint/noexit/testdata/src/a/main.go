package main

import "os"

func main() {
	os.Exit(0) // want "do not call os.Exit directly in func main"
}

func other() {
	os.Exit(1) // не в main — diagnostics не ожидается
}
