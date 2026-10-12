package main

import (
	"os"

	"github.com/NovaStepStudio/Minecraft-CodeDecompiler/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:]))
}
