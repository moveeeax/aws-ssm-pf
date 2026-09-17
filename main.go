package main

import (
	"fmt"
	"os"

	"github.com/moveeeax/aws-ssm-pf/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "ssm-pf:", err)
		os.Exit(1)
	}
}
